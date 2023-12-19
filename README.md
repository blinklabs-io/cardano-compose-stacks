# compose-stacks

This docker-compose setup provides a comprehensive environment for running Cardano related services. Below is a brief overview of the services and instructions on how to operate them.

## Services

- **cardano-node**: This is the main Cardano node service. It connects to the Cardano network specified by the NETWORK environment variable. By default, it connects to the `mainnet`.

- **cardano-node-api**: This service is responsible for interfacing with local Cardano node. It depends on the cardano-node service to be healthy before starting.

- **ogmios**: This service is a lightweight bridge interface for cardano-node. It provides an HTTP / WebSocket API that enables applications to interact with a local cardano-node via JSON+RPC-2.0. It depends on the cardano-node service to be healthy before starting.

- **tx-submit-api**: This service is responsible for submitting transactions to the Cardano network. It depends on the cardano-node service to be healthy before starting.

- **cardano-db-sync**: This service syncs the Cardano blockchain data to a PostgreSQL database. It depends on both the cardano-node and postgres services to be healthy before starting.

- **postgres**: This is the PostgreSQL database service used by the cardano-db-sync service to store the Cardano blockchain data.

## How to Start Services

Because each service has defined dependency that means starting a service will also start it's dependencies.

### Start Just the Cardano Node

To start only the cardano-node service, run:

```bash
docker compose up cardano-node
```

### Start Cardano Node and cardano-node-api

To start both the cardano-node and cardano-node-api services, run:

```bash
docker compose up cardano-node-api
```

### Start Cardano Node and tx-submit-api

To start both the cardano-node and tx-submit-api services, run:

```bash
docker compose up tx-submit-api
```

### Start Cardano Node and db-sync

To start both the cardano-node, cardano-db-sync and postgres services, run:

```bash
docker compose up cardano-db-sync
```

## How to Start All Services in Detached Mode

To start all services defined in the docker-compose file in detached mode, simply run:

```bash
docker compose up -d
```

This command will start all the services (i.e., cardano-node, tx-submit-api, cardano-db-sync, and postgres) in the background.

If you need to stop the services later, you can use:

```bash
docker compose down
```

This will stop and remove all the services started with docker-compose up. If you've started specific services and want to stop them, you can specify them in the down command, similar to the up command.
