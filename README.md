# redis-xkeys
A command line tools for statistic of redis keys based on scan command


## How to get

`go get -u github.com/ko-han/redis-xkeys`

## Example
```bash
» redis-cli set group_name value
OK
» redis-xkeys 0 group '^(.+)_(.+)$' '$1'
# Basic
start_at: 2022-05-17 19:26:53.846204147 +0800 CST m=+0.001160364
total_spend_time: 1.225106ms
total_scan_keys: 1
avg_key_size: 10
max_key_size: 10
last_cursor: 0

# Group ^(.+)_(.+)$ $1
group 1
<not-matched> 0

» redis-xkeys --help
redis-xkeys 0.3.0

redis-xkeys scans all redis keys and prints a briefing.

Usage: redis-xkeys [OPTIONS] cursor [MATCH pattern] [COUNT count] [TYPE type] 
                   [GROUP pattern replacement] [GROUPTYPE] [LIMIT limit]
                   [DISTINCT pattern]
  -a <password> Password to use when connecting to the server.
  -b    Start in batch mode, which could be useful for send output
                to other programs or to a file (default disable when STDOUT
                is not a tty).
  -h <hostname> Server hostname (default:127.0.0.1).
  --help        Output this help and exit.
  -i <interval> Waits <interval> seconds per scan. It is possible
                to specify sub-second times like -i 0.1.
  -n <db>       Database number.
  -p <port>     Server port (default:6379).
  --retry-times <retrytimes>    Retry times per scan when scan fails.
  --timeout <timeout>   Timeout in seconds for redis command (default:1).
  -u <interval> Update result every <interval> seconds (default:3).

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
Iterate at this most keys. If not set, xcan will iter all keys stored in redis server.

The DISTINCT option
It uses an regex pattern to match keys, increases count by one if matched.
Both matched count and un-matched count will be reported.
It possible to add more than one DISTINCT option.
```
