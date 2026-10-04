# Description

A 5 pools testnet using cardano-node version 10.5.4 testing consistency between nodes running UTxO-HD with LMDB and in-memory: We run 3 nodes with in-memory DB and 2 nodes with LMDB and check they are still consistent. A single Dingo node is added to the network as a client not producing blocks.

## Cardano-Node

- **Version**: 10.5.4
- **Branch**: -
- **Source/Compiled**: Compiled

## Dingo

- **Version**: 0.22.0_pre1-antithesis
- **Branch**: -
- **Source/Compiled**: Compiled

## Testnet

- **Pools**: 5

## Dingo Praos testnet

The Dingo mixed network harness lives in
[`dingo-praos/`](dingo-praos/README.md). It is a separate Moog testnet and
does not replace the UTxO-HD consistency stack described above.
