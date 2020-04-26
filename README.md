# tor-drop

A dropbox like service over tor. Deploy the service on your computer, own all the data, make it accessible as you want.

# usage

```go
$ go run . -h
  -assets string
    	assets directory (default "/assets/")
  -cookie string
    	secure cookie hashing secret (default "static")
  -csrf string
    	secure csrf hashing secret (default "static")
  -pk string
    	ed25519 pem encoded privatekey file path (default "onion.pk")
  -qps float
    	maximum http query per second (default 30)
  -static
    	use embedded static assets (default true)
  -storage string
    	path to the storage directory (default "data")
```

# demo

```sh
$ LOG=* go run -tags prod .
2020/04/25 00:31:42 public http://dv34gxugaym3olvkwfwydc3w3acn4dqap3cedvtzhi3oycc4lpcsqkad.onion/
00:31:42.050 file-server: starting tor-drop file server...
2020/04/25 00:31:42 admin  http://127.0.0.1:9091/
Apr 25 00:31:42.219 [notice] Tor 0.4.2.5 (git-bede4ea1008920d8) running on Linux with Libevent 2.1.8-stable, OpenSSL 1.1.1d, Zlib 1.2.11, Liblzma N/A, and Libzstd N/A.
...
Apr 25 00:31:50.000 [notice] Bootstrapped 100% (done): Done
127.0.0.1 - - [25/Apr/2020:00:32:21 +0200] "GET / HTTP/1.1" 200 523
...
```
You can browse the public interface at `http://dv34gxugaym3olvkwfwydc3w3acn4dqap3cedvtzhi3oycc4lpcsqkad.onion/`.

The administrator interface is available at `http://127.0.0.1:9091/`
