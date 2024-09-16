# parking-meter

## Queues and Storage

All services rely on either Redis or RabbitMQ, which can be started using the command `docker compose up redis rmq`.


## Services

### Entrance Notifier

The entrance-notifier service is responsible for notifying the system when a vehicle enters the parking lot.

#### Configuration Options
- `RMQ_URL`: RabbitMQ URL for message queue.
- `FIXTURE_PATH`: Path to the fixture file containing parking data.

#### Service Dependencies
- RabbitMQ

#### Start command
```sh
docker-compose up entrance-notifier
```

### Exit Notifier

The exit-notifier service is responsible for notifying the system when a vehicle exits the parking lot.


#### Configuration Options:

- `RMQ_URL`: RabbitMQ URL for message queue.
- `FIXTURE_PATH`: Path to the fixture file containing parking data.
- `MATCH_RATE`: Percentage of matching parkings.

#### Service Dependencies
- RabbitMQ

#### Start command

```sh
docker compose up exit-notifier
```


### Parkings Recorder

#### Description

parkings-recorder consumes the entrance and exit events of vehicles and stores them in a file.

Redis is used for storing parking state and distributed locking.

#### Configuration options

- `PORT`: port on which service will run.
- `TOKEN`: Bearer token.
- `STORAGE_FILE`: Path to a resulting file.


#### Metrics

Service exports the following metrics:
- `parking_recorder_queueing_latency_seconds`: Duration that a message spends in the queue.
- `parking_recorder_processing_latency_seconds`: Time taken to process the message.
- `parking_recorder_storage_latency_seconds`: Time taken to store the parking information.


#### Service Dependencies

- RabbitMQ
- Redis
- parkings-storage


#### Starting command

```sh
docker-compose up parkings-recorder
```


## Monitoring

Grafana and Prometheus can be started using the command `docker compose up prometheus grafana`. A simple dashboard for the parking-recorder service will be provisioned automatically.


## Areas of improvement

1. Implement tests for all of the services.
2. Improve services modules structuring.
3. Properly handle downtime of storage, queues and external services (e.g. storage).
4. Configure CI for quality checks.
