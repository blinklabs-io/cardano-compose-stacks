# Dingo Praos Antithesis Harness

This five pool testnet runs one Antithesis-instrumented Dingo producer with
four cardano-node producers. `testnets/dingo-praos/` is the Moog testnet
definition; the surrounding files provide the pool configurator, transaction
workload, and safety/liveness analyzer.

## Images

The Moog stack uses the published `main` images:

- `ghcr.io/blinklabs-io/dingo:main-antithesis`
- `ghcr.io/blinklabs-io/dingo-configurator:main`
- `ghcr.io/blinklabs-io/cardano-txpump:main`
- `ghcr.io/blinklabs-io/dingo-analysis:main`

The Dingo repository publishes its instrumented image. This repository builds
the configurator and analyzer in `.github/workflows/antithesis.yml`; the
separate `cardano-txpump` repository publishes the workload image.

## Local Compose run

From this directory:

```sh
./scripts/validate-config.sh
docker compose up -d
docker compose logs -f
docker compose down -v
```

The local compose file uses the Dingo `main-antithesis` image and the
`cardano-txpump:main` image. The Moog definition is under
`testnets/dingo-praos/` and adds fault handling and log analysis.

## Moog workflow

The scheduled and manually dispatched workflow runs from this repository's
`main` branch. It publishes the configurator and analyzer as `main` images,
then submits the current `main` commit and testnet directory to Moog. Configure
`MOOG_REQUESTER_WALLET` as a repository secret and set `MOOG_REQUESTER` as a
repository variable. `MOOG_MPFS_HOST` and `MOOG_TOKEN_ID` are optional.
