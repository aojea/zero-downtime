# zero-downtime

The idea is to be able to rollout a new web server process with the minimum disruption.

In order to do that we need to solve 2 problems, how to handle the following resources:

- network connections (sockets)

For network connections, the solutioin is based on Facebook paper on how to achieve zero disruption transferring file descriptors over a Unix domain socket. The problem here is much simpler, since it only cares about the listener socket, leaving the termination of the current connections problem to the actual server.

The same technique has been also implemented by haproxy, envoy and others proxies.

- log files

Log files can be renamed, they write to the file descriptor, so the secondary server needs to wait until the primary renames its logs file before creating its log.

## How it works:

The process use a server listening in a unix socket to synchronize the process and send the bind socket file descriptor.


## How to test it:

In one terminal launch one server:

```
./zero-downtime
I0727 16:04:40.312971 2120959 server.go:155] Socket doesn't exist, creating server on localhost:9090
I0727 16:04:40.318372 2120959 server.go:162] New listener created on address 127.0.0.1:9090
I0727 16:04:40.318551 2120959 server.go:34] Listen on socket /tmp/zero-downtime.sock for zero downtime
```

from another terminal start to create connections to the server:

```
ab -r -c 1 -n200000 http://localhost:9090/                                                                                                                       
This is ApacheBench, Version 2.3 <$Revision: 1879490 $>                                                                                                                             
Copyright 1996 Adam Twiss, Zeus Technology Ltd, http://www.zeustech.net/                                                                                                            
Licensed to The Apache Software Foundation, http://www.apache.org/                                                                                                                  

Benchmarking localhost (be patient)
Completed 20000 requests
Completed 40000 requests
Completed 60000 requests
Completed 80000 requests
Completed 100000 requests
Completed 120000 requests
```

before the `ab` command ends, spawn a second server that takes over the first one:
```
/zero-downtime
I0727 16:05:25.053310 2121091 server.go:99] Socket exists /tmp/zero-downtime.sock trying to get the listener
I0727 16:05:25.053437 2121091 server.go:110] Connected on /tmp/zero-downtime.sock, getting file descriptor
I0727 16:05:25.053574 2121091 server.go:126] Waiting for listener file descriptor on uds /tmp/zero-downtime.sock
I0727 16:05:25.053854 2121091 server.go:143] Got socket fd 8
I0727 16:05:25.053878 2121091 server.go:152] New listener created from fd 8 on address 127.0.0.1:9090
I0727 16:05:25.054046 2121091 server.go:34] Listen on socket /tmp/zero-downtime.sock for zero downtime
```

see how the `ab` command is able to keep using it without disruption:
```
Completed 140000 requests
Completed 160000 requests
Completed 180000 requests
Completed 200000 requests
Finished 200000 requests


Server Software:        
Server Hostname:        localhost
Server Port:            9090

Document Path:          /
Document Length:        271 bytes

Concurrency Level:      1
Time taken for tests:   32.548 seconds
Complete requests:      200000
Failed requests:        1
   (Connect: 0, Receive: 1, Length: 0, Exceptions: 0)
Total transferred:      86799566 bytes
HTML transferred:       54199729 bytes
Requests per second:    6144.85 [#/sec] (mean)
Time per request:       0.163 [ms] (mean)
Time per request:       0.163 [ms] (mean, across all concurrent requests)
Transfer rate:          2604.35 [Kbytes/sec] received

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0    0   0.1      0      14
Processing:     0    0   0.2      0      28
Waiting:        0    0   0.2      0      13
Total:          0    0   0.2      0      28

Percentage of the requests served within a certain time (ms)
  50%      0
  66%      0
  75%      0
  80%      0
  90%      0
  95%      0
  98%      0
  99%      0
 100%     28 (longest request)
```

and the first server dies gracefully:
```
...
I0727 16:04:49.112842 2120995 server.go:34] Listen on socket /tmp/zero-downtime.sock for zero downtime
I0727 16:05:25.053816 2120995 server.go:63] Read new data from connection: ZERO
I0727 16:05:25.053827 2120995 server.go:71] ZERO DOWNTIME starting, connection received on /tmp/zero-downtime.sock from @
I0727 16:05:25.053885 2120995 server.go:194] Gracefully stopping
I0727 16:05:30.053996 2120995 server.go:242] Exiting ........
```

## References:

1. https://blog.apnic.net/2021/03/29/how-facebook-achieves-disruption-free-updates-with-zero-downtime/
2. https://copyconstruct.medium.com/file-descriptor-transfer-over-unix-domain-sockets-dcbbf5b3b6ec
3. https://www.haproxy.com/blog/truly-seamless-reloads-with-haproxy-no-more-hacks/
4. https://gist.github.com/kirk91/ec25703848172e8f56f671e0e1c73751
5. https://blog.envoyproxy.io/envoy-hot-restart-1d16b14555b5
6. https://blog.cloudflare.com/know-your-scm_rights/