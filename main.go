package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/go-redis/redis/v8"
	"go.uber.org/automaxprocs/maxprocs"
)

var config = struct {
	Hostname   string  `opt:"h" help:"Server hostname"`
	Port       uint16  `opt:"p" help:"Server port"`
	Password   string  `opt:"a" help:"Password to use when connecting to the server" desc:"password"`
	Interval   float64 `opt:"i" help:"Waits <interval> seconds per scan. It is possible to specify sub-second times like -i 0.1"`
	DB         int64   `opt:"n" help:"Database number"`
	Help       bool    `opt:"help" help:"Output this help and exit"`
	RetryTimes int64   `opt:"retry-times" help:"Retry times per scan when scan fails"`
	BatchMode  bool    `opt:"b" help:"Start in batch mode, which could be useful for send output to other programs or to a file (default disable when STDOUT is not a tty)"`
	Update     float64 `opt:"u" help:"Update result every <interval> seconds" desc:"interval"`
	Timeout    float64 `opt:"timeout" help:"Timeout in seconds for redis command"`
	Arguments  struct {
		Cursor   uint64
		Match    string
		Limit    uint64
		Type     *string
		Count    int64
		Group    [][2]string
		Distinct []string
	}
}{
	Hostname: "127.0.0.1",
	Port:     6379,
	Update:   3,
	Timeout:  1,
}

// -- uint Value
type uint16Value uint16

func newUint16Value(val uint16, p *uint16) *uint16Value {
	*p = val
	return (*uint16Value)(p)
}

func (i *uint16Value) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, strconv.IntSize)
	if err != nil {
		err = errors.New("parse number fails")
	}
	*i = uint16Value(v)
	return err
}

func (i *uint16Value) Get() interface{} { return uint16(*i) }

func (i *uint16Value) String() string { return strconv.FormatUint(uint64(*i), 10) }

const usage = `redis-xkeys 0.5.0

redis-xkeys scans all redis keys and prints a briefing.

Usage: redis-xkeys [OPTIONS] cursor [MATCH pattern] [COUNT count] [TYPE type] 
                   [GROUP pattern replacement] [GROUPTYPE] [LIMIT limit]
                   [DISTINCT pattern]
`
const usageSuffix = `
The Match option
As same as redis scan match option.

The COUNT option
As same as redis scan count option.

The TYPE option
As same as redis scan type option. A type call to every key if redis-server 
do  not support type option.

The GROUP option
It uses an regex pattern to match and classify keys into groups.
Regex sub-group matches could get by ${groupname} or ${groupindex}.
It possible to add more than one groups.

The GROUPTYPE option
It prints keys count as they are grouped by their type.

The LIMIT option
Iterate at this most keys. If not set, xscan will iter all keys stored in redis server.

The DISTINCT option
It uses an regex pattern to match keys, increases count by one if matched.
Both matched count and un-matched count will be reported.
It possible to add more than one DISTINCT option.
`

func newflag() *flag.FlagSet {
	fg := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fg.SetOutput(os.Stderr)

	fg.Usage = func() {
		fmt.Fprint(fg.Output(), usage)
		fg.VisitAll(func(flag *flag.Flag) {
			var b strings.Builder
			if len(flag.Name) == 1 {
				fmt.Fprintf(&b, "  -%s ", flag.Name) // Two spaces before -; see next two comments.
			} else {
				fmt.Fprintf(&b, "  --%s ", flag.Name)
			}
			usage := flag.Usage
			if len(flag.Usage) > 60 {
				var hb strings.Builder
				var runeWrites int
				for _, r := range flag.Usage {
					if runeWrites < 55 { // find space in 5 characters.
						hb.WriteRune(r)
						runeWrites++
						continue
					}
					if r < math.MaxUint8 && r != ' ' {
						hb.WriteRune(r)
						runeWrites++
						continue
					}
					runeWrites = 0
					hb.WriteString("\n\t\t")
					if r != ' ' {
						hb.WriteRune(r)
					}
				}
				usage = hb.String()
			}
			b.WriteString(usage)
			fmt.Fprint(fg.Output(), b.String(), "\n")
		})
		fmt.Fprint(fg.Output(), usageSuffix)
	}
	return fg
}

func parseOption(args []string) {
	if len(args) == 0 {
		assert(errors.New("ERR syntax error"))
	}
	cursor := args[0]
	toUint := func(s string) uint64 {
		i, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			assert(errors.New("ERR syntax error"))
		}
		return i
	}

	config.Arguments.Cursor = toUint(cursor)

	args = args[1:]
	for i := 0; i < len(args); i++ {
		getNext := func() string {
			i++
			if len(args) == i {
				assert(errors.New("ERR syntax error"))
			}
			return args[i]
		}
		c := strings.ToLower(args[i])
		switch c {
		case "match":
			config.Arguments.Match = getNext()
		case "limit":
			config.Arguments.Limit = toUint(getNext())
		case "group":
			pa := getNext()
			repl := getNext()
			config.Arguments.Group = append(config.Arguments.Group, [2]string{pa, repl})
		case "type":
			tp := getNext()
			config.Arguments.Type = &tp
		case "grouptype":
			if config.Arguments.Type == nil {
				a := ""
				config.Arguments.Type = &a
			}
		case "distinct":
			pa := getNext()
			config.Arguments.Distinct = append(config.Arguments.Distinct, pa)
		default:
			assert(errors.New("ERR syntax error"))
		}
	}
}

func initFlag() {
	fg := newflag()
	value := reflect.ValueOf(&config)
	value = value.Elem()
	numfield := value.NumField()
	for i := 0; i < numfield; i++ {
		f := value.Field(i)
		sf := value.Type().Field(i)
		opt := sf.Tag.Get("opt")
		if opt == "" {
			continue
		}
		desc, ok := sf.Tag.Lookup("desc")
		if !ok {
			desc = strings.ToLower(sf.Name)
		}
		if f.Kind() == reflect.Bool {
			desc = "" // bool value should not contains any type description
		}
		help := sf.Tag.Get("help")

		var usage strings.Builder
		if desc != "" {
			fmt.Fprintf(&usage, "<%s>", desc)
		}
		usage.WriteRune('\t')
		usage.WriteString(help)
		if !f.IsZero() {
			fmt.Fprintf(&usage, " (default:%v)", f.Interface())
		}
		usage.WriteRune('.')

		switch f.Kind() {
		case reflect.String:
			ptr := f.Addr().Interface().(*string)
			fg.StringVar(ptr, opt, *ptr, usage.String())
		case reflect.Uint16:
			ptr := f.Addr().Interface().(*uint16)
			fg.Var(newUint16Value(*ptr, ptr), opt, usage.String())
		case reflect.Bool:
			ptr := f.Addr().Interface().(*bool)
			fg.BoolVar(ptr, opt, *ptr, usage.String())
		case reflect.Float64:
			ptr := f.Addr().Interface().(*float64)
			fg.Float64Var(ptr, opt, *ptr, usage.String())
		case reflect.Int64:
			ptr := f.Addr().Interface().(*int64)
			fg.Int64Var(ptr, opt, *ptr, usage.String())
		default:
			panic(fmt.Sprintf("unsupported type(%s):%s", sf.Name, f.Kind()))
		}
	}

	_ = fg.Parse(os.Args[1:])
	if config.Help {
		fg.Usage()
		os.Exit(0)
	}
	parseOption(fg.Args())
}

type Interceptor interface {
	Apply(rdb *redis.Client, cursor uint64, keys []string) ([]string, bool)
	Result(w io.Writer) error
}
type Interceptors []Interceptor

func (i Interceptors) Apply(rdb *redis.Client, cursor uint64, keys []string) bool {
	var continues bool
	for _, c := range i {
		keys, continues = c.Apply(rdb, cursor, keys)
		if !continues {
			return false
		}
	}
	return true
}

func (i Interceptors) Result() {
	var buf bytes.Buffer
	for _, c := range i {
		c.Result(&buf)
		buf.WriteByte('\n')
	}
	flush(&buf)
}

type Basic struct {
	start      time.Time
	keys       uint64
	maxKeySize int
	avgKeySize float64
	cursor     uint64
}

var _ Interceptor = (*Basic)(nil)

func NewBasic() *Basic {
	return &Basic{
		start: time.Now(),
	}
}

func (t *Basic) Apply(rdb *redis.Client, cursor uint64, keys []string) ([]string, bool) {
	var totalsize float64
	for _, k := range keys {
		if len(k) > t.maxKeySize {
			t.maxKeySize = len(k)
		}
		totalsize += float64(len(k))
	}
	t.avgKeySize = (t.avgKeySize*float64(t.keys) + totalsize) / (float64(t.keys) + float64(len(keys)))
	t.keys += uint64(len(keys))
	return keys, true
}

func (t *Basic) Result(w io.Writer) error {
	fmt.Fprintln(w, "# Basic")
	fmt.Fprintf(w, "start_at:%s\n", t.start)
	fmt.Fprintf(w, "total_spend_time:%s\n", time.Since(t.start))
	fmt.Fprintf(w, "total_scan_keys:%d\n", t.keys)
	fmt.Fprintf(w, "scan_keys_speed:%.f/s\n", float64(t.keys)/time.Since(t.start).Seconds())
	fmt.Fprintf(w, "avg_key_size:%.0f\n", t.avgKeySize)
	fmt.Fprintf(w, "max_key_size:%d\n", t.maxKeySize)
	fmt.Fprintf(w, "last_cursor:%d\n", t.cursor)
	return nil
}

type Groups struct {
	count      map[string]uint64
	notMatched uint64

	pattern    *regexp.Regexp
	patternStr string
	template   string
}

var _ Interceptor = (*Groups)(nil)

func NewGroup(pattern, replace string) (*Groups, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Groups{
		patternStr: pattern,
		pattern:    re,
		template:   replace,
		count:      map[string]uint64{},
	}, nil
}

type kv struct {
	k string
	v uint64
}
type kvs []kv

func (k kvs) Len() int {
	return len(k)
}
func (k kvs) Swap(i, j int) {
	k[i], k[j] = k[j], k[i]
}

func (k kvs) Less(i, j int) bool {
	return k[i].v < k[j].v
}

func (s *Groups) Result(w io.Writer) error {
	fmt.Fprintf(w, "# Group %s %s\n", s.patternStr, s.template)
	var item = make(kvs, len(s.count))
	var i int
	for k, v := range s.count {
		item[i] = kv{k: k, v: v}
		i++
	}
	sort.Sort(sort.Reverse(item))
	for _, i := range item {
		fmt.Fprintf(w, "%s:%d\n", i.k, i.v)
	}
	fmt.Fprintf(w, "<not-matched>:%d\n", s.notMatched)
	return nil
}

func (s *Groups) Apply(rdb *redis.Client, cursor uint64, keys []string) ([]string, bool) {
	for _, key := range keys {
		matches := s.pattern.FindAllStringSubmatchIndex(key, -1)
		for _, submatches := range matches {
			result := s.pattern.ExpandString(nil, s.template, key, submatches)
			s.count[string(result)]++
		}
		if len(matches) == 0 {
			s.notMatched++
		}
	}
	return keys, true
}

type Limiter struct {
	max, cur uint64
}

func NewLimiter(max uint64) *Limiter {
	return &Limiter{max: max}
}

var _ Interceptor = (*Limiter)(nil)

func (l *Limiter) Apply(rdb *redis.Client, cursor uint64, keys []string) ([]string, bool) {
	ok := l.cur <= l.max
	l.cur += uint64(len(keys))
	return keys, ok
}

func (l *Limiter) Result(io.Writer) error {
	return nil
}

type Typer struct {
	restriction string
	counts      map[string]uint64
}

var _ Interceptor = (*Typer)(nil)

func NewTyper(restriction string) *Typer {
	return &Typer{restriction: restriction, counts: map[string]uint64{}}
}

func (t *Typer) Apply(rdb *redis.Client, cursor uint64, keys []string) ([]string, bool) {
	if len(keys) == 0 {
		return keys, true
	}
	var (
		wg    sync.WaitGroup
		errs  = make([]error, len(keys))
		types = make([]string, len(keys))
	)
	for i, k := range keys {
		wg.Add(1)
		go func(idx int, key string) {
			defer wg.Done()
			var (
				err error
				tp  string
			)
			err = retry(func() error {
				tp, err = rdb.Type(context.Background(), key).Result()
				return err
			})
			errs[idx] = err
			types[idx] = tp
		}(i, k)
	}
	wg.Wait()
	for _, e := range errs {
		assert(e)
	}
	for _, k := range types {
		t.counts[k]++
	}
	if t.restriction != "" {
		var k []string
		for i, v := range types {
			if v == t.restriction {
				k = append(k, keys[i])
			}
		}
		return k, true
	}
	return keys, true
}

func (t *Typer) Result(w io.Writer) error {
	fmt.Fprintln(w, "# Type")
	for k, v := range t.counts {
		fmt.Fprintf(w, "%s:%d\n", k, v)
	}
	return nil
}

type Distinct struct {
	patternStr string
	matched    uint64
	notMatched uint64
	pattern    *regexp.Regexp
}

var _ Interceptor = (*Distinct)(nil)

func NewDistinct(pattern string) (*Distinct, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Distinct{patternStr: pattern, pattern: re}, nil
}

func (d *Distinct) Apply(rdb *redis.Client, cursor uint64, keys []string) ([]string, bool) {
	for _, k := range keys {
		if d.pattern.MatchString(k) {
			d.matched++
		} else {
			d.notMatched++
		}
	}
	return keys, true
}

func (d *Distinct) Result(w io.Writer) error {
	fmt.Fprintf(w, "# Distinct %s\n", d.patternStr)
	fmt.Fprintf(w, "matched:%d\n", d.matched)
	fmt.Fprintf(w, "unmatched:%d\n", d.notMatched)
	return nil
}

func assert(err error) {
	if err != nil {
		finish()
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		quit(1)
	}
}

// This should be changed when uses a tty.
var (
	beforeQuit = func() {}
	finish     = func() {}
	flush      = func(b *bytes.Buffer) {
		if b.Bytes()[len(b.Bytes())-1] != '\n' {
			b.WriteByte('\n')
		}
		os.Stdout.Write(b.Bytes())
		os.Stdout.Sync()
	}
)

func quit(code int) {
	finish()
	beforeQuit()
	os.Exit(code)
}

func setStdOut() {
	flush = func(b *bytes.Buffer) {
		os.Stdout.Write(b.Bytes())
		os.Stdout.Sync()
	}
}

func setupTTY() bool {
	if config.BatchMode {
		return false
	}
	// Initialize screen
	s, err := tcell.NewScreen()
	if err != nil {
		return false
	}
	if err := s.Init(); err != nil {
		return false
	}
	s.DisableMouse()
	s.DisablePaste()
	finish = func() {
		s.Fini()
	}
	flush = func(b *bytes.Buffer) {
		s.Clear()
		for i := 0; ; i++ {
			d, err := b.ReadBytes('\n')
			s.SetContent(0, i, 0, []rune(string(d)), tcell.StyleDefault)
			if err != nil {
				break
			}
		}
		s.Show()
	}
	go func() {
		for {
			// Poll event
			ev := s.PollEvent()
			// Process event
			switch ev := ev.(type) {
			case *tcell.EventResize:
				s.Sync()
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC {
					quit(0)
				}
			}
		}
	}()
	return true
}

func setupOutput() {
	if setupTTY() {
		return
	}
	c := make(chan os.Signal, 10)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for range c {
			quit(8)
		}
	}()
}

func retry(f func() error) error {
	if config.RetryTimes <= 0 {
		return f()
	}
	var err error
	for i := int64(0); i < config.RetryTimes; i++ {
		err := f()
		if _, ok := err.(redis.Error); ok {
			return err
		}
		if err != nil {
			return nil
		}
	}
	return err
}

func Connect(opt *redis.Options) (*redis.Client, error) {
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping error:%w", err)
	}
	return rdb, nil
}

func SupportScanType(rdb *redis.Client) (supported bool, err error) {
	err = retry(func() error {
		err = rdb.ScanType(context.Background(), 0, "", 0, "string").Err()
		if _, ok := err.(redis.Error); ok { // redis don't support type
			return nil
		}
		return err
	})
	return
}

func ScanWithRetry(rdb *redis.Client,
	interceptors Interceptors, typ string) error {
	var (
		keys       []string
		cursor     = config.Arguments.Cursor
		tempcursor uint64
		err        error
	)
	start := time.Now()
	every := time.Duration(float64(time.Second) * config.Update)
	if every <= 0 {
		every = time.Second * 3
	}
	var sleep = func() {}
	if config.Interval > 0 {
		du := time.Duration(float64(time.Second) * config.Interval)
		sleep = func() {
			time.Sleep(du)
		}
	}
	ctx := context.Background()
	for {
		err = retry(func() error {
			if typ != "" {
				keys, tempcursor, err = rdb.ScanType(ctx, cursor, config.Arguments.Match,
					config.Arguments.Count, typ).Result()
			} else {
				keys, tempcursor, err = rdb.Scan(ctx, cursor, config.Arguments.Match,
					config.Arguments.Count).Result()
			}
			return err
		})
		if err != nil {
			return fmt.Errorf("scan error:%w", err)
		}
		cursor = tempcursor
		if !interceptors.Apply(rdb, cursor, keys) {
			break
		}
		if cursor == 0 {
			break
		}
		if time.Since(start) > every {
			interceptors.Result()
			start = time.Now()
		}
		sleep()
	}
	return nil
}

func main() {
	maxprocs.Set()
	initFlag()

	timeout := time.Duration(float64(time.Second) * config.Timeout)
	if timeout < 0 {
		timeout = time.Second
	}
	rdb, err := Connect(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", config.Hostname, config.Port),
		Password:     config.Password,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		DB:           int(config.DB),
	})
	assert(err)
	var interceptors Interceptors
	interceptors = append(interceptors, NewBasic())
	var typ string
	if config.Arguments.Type != nil {
		typ = *config.Arguments.Type
		if typ != "" {
			ok, err := SupportScanType(rdb)
			assert(err)
			if ok {
				interceptors = append(interceptors, NewTyper(""))
			}
		} else {
			interceptors = append(interceptors, NewTyper(""))
		}
	}

	if config.Arguments.Limit > 0 {
		interceptors = append(interceptors, NewLimiter(config.Arguments.Limit))
	}

	for _, v := range config.Arguments.Group {
		g, err := NewGroup(v[0], v[1])
		assert(err)
		interceptors = append(interceptors, g)
	}
	for _, v := range config.Arguments.Distinct {
		g, err := NewDistinct(v)
		assert(err)
		interceptors = append(interceptors, g)
	}

	setupOutput()
	beforeQuit = func() {
		setStdOut()
		interceptors.Result()
	}
	err = ScanWithRetry(rdb, interceptors, typ)
	assert(err)
	quit(0)
}
