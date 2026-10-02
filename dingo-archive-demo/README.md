# Dingo Archive Node Demo

This stack demonstrates a Dingo archive node, a History Expiry node, MinIO
storage, and Bark proxying. The MinIO archive retains blocks after the pruning
node expires its local copies; Bark serves those blocks on demand.

## Run

```sh
./demo.sh          # guided demo with live storage and expiry stats
./start.sh         # start the stack for manual exploration
./run-tests.sh     # run the Docker-backed integration scenario
./stop.sh          # stop and remove the stack
```

The integration scenario needs Docker Compose and uses the default host ports
shown below. It keeps the Docker-backed Go test behind the `archive_demo` build
tag so a regular `go test ./...` does not try to manage Docker containers.

## Stack

- `cardano-producer` — cardano-node 11.0.1, producing the test chain
- `dingo-archive` — Dingo with S3 blob storage and Bark server
- `dingo-pruning` — Dingo with Badger storage, History Expiry, and a Bark client
- `minio` — S3-compatible storage for the archived block data

The testnet uses `k=40` and `f=0.4`, so the stability window is 300 slots.
The demo waits until the chain advances beyond that window, then fetches an
expired block through the Bark proxy and checks that its CBOR is stored in
MinIO but absent from the pruning node's local Badger store.

## Ports

| Component | Default host port | Environment override |
|---|---:|---|
| cardano-producer | 3110 | `ARCHIVEDEMO_CARDANO_PORT` |
| dingo-archive | 3111 | `ARCHIVEDEMO_DINGO_ARCHIVE_PORT` |
| Bark gRPC | 3112 | `ARCHIVEDEMO_BARK_PORT` |
| dingo-pruning | 3113 | `ARCHIVEDEMO_DINGO_PRUNING_PORT` |
| MinIO API | 9100 | `ARCHIVEDEMO_MINIO_PORT` |
| MinIO console | 9101 | `ARCHIVEDEMO_MINIO_CONSOLE_PORT` |

MinIO credentials are `demo` / `demodemo`. Set `DINGO_IMAGE` to use a
different Dingo container image.
