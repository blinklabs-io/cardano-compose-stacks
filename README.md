# compose-stacks

This docker-compose setup provides a comprehensive environment for running Cardano related services. Below is a brief overview of the services and instructions on how to operate them.

## Services

- **cardano-node**: This is the main Cardano node service. It connects to the Cardano network specified by the NETWORK environment variable. By default, it connects to the `mainnet`.

- **cardano-node-api**: This service is responsible for interfacing with local Cardano node. It depends on the cardano-node service to be healthy before starting.

- **bursa**: This service is programatic wallet. It runs without any persistence.

- **ogmios**: This service is a lightweight bridge interface for cardano-node. It provides an HTTP / WebSocket API that enables applications to interact with a local cardano-node via JSON+RPC-2.0. It depends on the cardano-node service to be healthy before starting.

- **tx-submit-api**: This service is responsible for submitting transactions to the Cardano network. It depends on the cardano-node service to be healthy before starting.

- **cardano-db-sync**: This service syncs the Cardano blockchain data to a PostgreSQL database. It depends on both the cardano-node and postgres services to be healthy before starting.

- **postgres**: This is the PostgreSQL database service used by the cardano-db-sync service to store the Cardano blockchain data.

## How to Start Services

Because each service has defined dependency that means starting a service will also start it's dependencies.

### Using Profiles in Docker Compose

With profiles, you can selectively start services based on different needs or environments.
Below are examples of how to use profiles in this setup.

### Start Just the Cardano Node

To start only the `cardano-node` service, which is part of the `node` profile, run:

```bash
docker compose --profile node up
```

### Start Cardano Node and cardano-node-api

To start both the `cardano-node` and `cardano-node-api` use `node-api` profile, run:

```bash
docker compose --profile node-api up
```

### Start Cardano Node and tx-submit-api

To start both the `cardano-node` and `tx-submit-api` use `tx-submit-api` profile, run:

```bash
docker compose --profile tx-submit-api up
```

### Start Cardano Node and db-sync

To start both the `cardano-node` and `cardano-db-sync` use `db-sync` profile, run:

```bash
docker compose --profile db-sync up
```

### Start All Services in Detached Mode

To start all services defined in the `docker-compose.yml` file in detached mode, run:

docker compose up -d

This command will start all services (e.g., `cardano-node`, `tx-submit-api`, `cardano-db-sync`, and `postgres`) in the background, regardless of profiles.
If you need to stop the services later, use:

docker compose down

This will stop and remove all the services started with `docker compose up`.
If you've started specific services using profiles and want to stop them, you can specify the same profiles in the `down` command.

### How to Use Bursa

To start just the `bursa` service, which is part of the `bursa` profile, run:

```bash
docker compose --profile bursa up
```

**Access Swagger UI:**

Open your web browser and navigate to the Swagger UI:

<http://localhost:8090/swagger/index.html>

**Execute a Create Request using Swagger UI:**

In the Swagger UI, find the section for creating a new wallet.
Click on the `Get` `/api/v1/wallet/create` operation.
Choose `Try it out`.
Click `Execute`.

This will send a create request to Bursa, and you should receive a JSON response with the details of the newly created wallet.

Store the mnemonic in a safe place. If you want to restore the wallet, you will need the mnemonic. If you lose the mnemonic, you will lose access to the wallet.

### How to Use Bluefin

To start just the `bluefin` service, which is part of the `bluefin` profile, run:

```bash
docker compose --profile bluefin up
```

to start the `bluefin-inspector` service, which is part of the `bluefin-inspector` profile, run:

```bash
docker compose --profile bluefin-inspector up
```

to start both the `bluefin` and `bluefin-inspector` services, use `bluefin` and `bluefin-inspector` profile, run:

```bash
docker compose --profile bluefin --profile bluefin-inspector up
```

to see the seed phrase of the wallet created by bluefin, run:

```bash
 docker exec bluefin-inspector cat /data/seed.txt
```

Bluefin-inspector is a service that will allow you to see the seed phrase of the wallet created by bluefin.
Seed phrase will be stored and managed on the local filesystem of the Docker host.
The bluefin-inspector is setup to run for an 1h. After that, it will stop automatically.

You can restart it by running the command below.

```bash
docker compose --profile bluefin-inspector up -d --force-recreate
```

### How to Use Cardano Wallet

To start just the `cardano-wallet` service, which is part of the `wallet` profile, run:

```bash
docker compose --profile wallet up
```

## Version Maintenance

To check for upstream image updates and refresh default versions in the compose file, run:

```bash
./scripts/check-versions.sh
```

To validate that all configured compose variables exist with defaults:

```bash
./scripts/validate-versions.sh
```

To update a specific service version:

```bash
./scripts/add-version.sh cardano-node 10.6.2
```

You can also use the Makefile targets:

```bash
make check-versions
make validate-versions
make add-version PACKAGE=cardano-node VERSION=10.6.2
```
