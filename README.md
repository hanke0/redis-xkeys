# redis-key-stats
A command line tools for statistic of redis keys based on scan command


## How to get

`go get -u github.com/ko-han/redis-key-stats`

## Example
```bash
» redis-cli set 1_a aa
OK
» redis-key-stats '(.*)_a' '$1'
#pattern number
a 1

scan keys: 1, spend: 1.881753ms
» ./redis-key-stats --help
Usage: main [Options...] pattern replace

patter is an regex expression and replace is key statistic group result. 
See an example of group keys by it's prefix:
pattern is '(.*)_.*' and replace is '$1'

Options:
  -a string
        Password to use when connecting to the server
  -h string
        Server hostname (default "localhost")
  -m string
        Redis scan match pattern
  -max uint
        Max keys to scan (default 18446744073709551615)
  -p int
        Server port (default 6379)
  -r uint
        Retry times (default 10)
```
