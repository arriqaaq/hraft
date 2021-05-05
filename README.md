# hraft

hraft is a reference implementation of [Hashicorp Raft implementation](https://github.com/hashicorp/raft). The storage backend is [Buntdb](https://github.com/tidwall/buntdb).

## Reading and writing keys

The reference implementation is a very simple in-memory key-value store. You can set a key by sending a request to the HTTP bind address (which defaults to `localhost:8080`):
```bash
curl -XPOST localhost:8080/key -d '{"foo": "bar"}'
```

You can read the value for a key like so:
```bash
curl -XGET localhost:8080/key/foo
```

## Running hraft

Starting and running a hraft cluster is easy. Download hraft like so:
```bash
git clone https://github.com/arriqaaq/hraft.git
cd hraft/
go build
```

Run your first hraft node like so:
```bash
$GOPATH/bin/hraft -id node0 ~/node0
```

You can now set a key and read its value back:
```bash
curl -XPOST localhost:8080/key -d '{"user1": "aly"}'
curl -XGET localhost:8080/key/user1
```

### Bring up a cluster

Bring up three terminals

##### Terminal 1
You should start the first node like so:
```
$GOPATH/bin/hraft -id node1 -haddr 127.0.0.1:8080 -raddr 127.0.0.1:9000 ~/node1
```
This way the node is listening on an address reachable from the other nodes. This node will start up and become leader of a single-node cluster.

##### Terminal 2
Next, start the second node as follows:
```
$GOPATH/bin/hraft -id node2 -haddr 127.0.0.1:8081 -raddr 127.0.0.1:9001 -join 127.0.0.1:8080 ~/node2
```

##### Terminal 3
Finally, start the third node as follows:
```
$GOPATH/bin/hraft -id node2 -haddr 127.0.0.1:8082 -raddr 127.0.0.1:9002 -join 127.0.0.1:8080 ~/node3
```
This tells each new node to join the existing node. Once joined, each node now knows about the key:
```bash
curl -XGET localhost:8080/key/user1
curl -XGET localhost:8081/key/user1
curl -XGET localhost:8082/key/user1
```

Furthermore you can add a second key:
```bash
curl -XPOST localhost:8080/key -d '{"user2": "bloom"}'
```

Confirm that the new key has been set like so:
```bash
curl -XGET localhost:8080/key/user2
curl -XGET localhost:8081/key/user2
curl -XGET localhost:8082/key/user2
```
