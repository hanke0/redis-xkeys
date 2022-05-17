# redis-key-stats
A command line tools for statistic of redis keys based on scan command


## How to get

`go get -u github.com/ko-han/redis-xscan`

## Example
```bash
» redis-cli set group_name value
OK
» redis-xscan 0 group '^(.+)_(.+)$' '$1'
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

» redis-xcan --help

```
