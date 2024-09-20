package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/bsm/redislock"
	_ "github.com/joho/godotenv/autoload"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/round-cube/parking-meter/shared"
	log "github.com/sirupsen/logrus"
)

var (
	QueueingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "parking_recorder_queueing_latency_seconds",
		Help:    "Time an event spends in RabbitMQ before being consumed by parking recorder",
		Buckets: prometheus.DefBuckets,
	})

	ProcessingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "parking_recorder_processing_latency_seconds",
		Help:    "Time an event spends being processed by parking recorder",
		Buckets: prometheus.DefBuckets,
	})

	StorageLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "parking_recorder_storage_latency_seconds",
		Help:    "Time an event spends being stored by parking recorder",
		Buckets: prometheus.DefBuckets,
	})
)

type Settings struct {
	redisURL            string
	rmqURL              string
	entrancesQueueName  string
	exitsQueueName      string
	httpRequestTimeoutS int
	storageURL          string
	storageToken        string
	entriesWorkers      int
	exitWorkers         int
	promPort            int
	promPath            string
}

func newSettings() (Settings, error) {
	var s Settings
	var err error

	s.rmqURL, err = getEnv("RMQ_URL")
	if err != nil {
		return s, err
	}

	s.redisURL, err = getEnv("REDIS_URL")
	if err != nil {
		return s, err
	}

	s.httpRequestTimeoutS = getEnvInt("HTTP_REQUEST_TIMEOUT_S", 10)
	s.entriesWorkers = getEnvInt("ENTRIES_WORKERS", 10)
	s.exitWorkers = getEnvInt("EXIT_WORKERS", 10)
	s.entrancesQueueName = getEnvDefault("ENTRANCES_QUEUE_NAME", "entrances")
	s.exitsQueueName = getEnvDefault("EXITS_QUEUE_NAME", "exits")
	s.storageURL = getEnvDefault("STORAGE_URL", "http://127.0.0.1:8000")
	s.storageToken = getEnvDefault("STORAGE_TOKEN", "parking-meter")
	s.promPath = getEnvDefault("PROM_PATH", "/metrics")
	s.promPort = getEnvInt("PROM_PORT", 2112)

	return s, nil
}

func getEnv(key string) (string, error) {
	value, set := os.LookupEnv(key)
	if !set {
		return "", fmt.Errorf("environment variable must be set: %s", key)
	}
	return value, nil
}

func getEnvDefault(key, defaultValue string) string {
	if value, set := os.LookupEnv(key); set {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value, set := os.LookupEnv(key); set {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

type Worker struct {
	Redis    *redis.Client
	Locker   *redislock.Client
	LockOpts *redislock.Options
	Id       int
	Settings *Settings
}

type Parking struct {
	EntryDateTime time.Time `json:"start_date_time"`
	ExitDateTime  time.Time `json:"end_date_time"`
	Status        string
	VehiclePlate  string `json:"vehicle_plate"`
}

func NewParkingState() *Parking {
	return &Parking{}
}

func (p *Parking) IsEmpty() bool {
	return p.Status == "empty"
}

func (w *Worker) GetParking(id string) (*Parking, error) {
	ctx := context.Background()
	state, err := w.Redis.HGetAll(ctx, id).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch parking state from Redis: %s", err)
	}

	parking := &Parking{}
	if len(state) == 0 {
		return parking, nil
	}

	if entryTime, ok := state["entry_date_time"]; ok && entryTime != "" {
		if parking.EntryDateTime, err = time.Parse(time.RFC3339, entryTime); err != nil {
			return nil, fmt.Errorf("failed to parse state entry date time: %s", err)
		}
	}

	if exitTime, ok := state["exit_date_time"]; ok && exitTime != "" {
		if parking.ExitDateTime, err = time.Parse(time.RFC3339, exitTime); err != nil {
			return nil, fmt.Errorf("failed to parse state exit date time: %s", err)
		}
	}

	parking.Status = state["status"]
	parking.VehiclePlate = state["vehicle_plate"]
	return parking, nil
}

func (w *Worker) SaveParking(p *Parking, id string) error {
	ctx := context.Background()
	state := map[string]string{
		"entry_date_time": formatTime(p.EntryDateTime),
		"exit_date_time":  formatTime(p.ExitDateTime),
		"status":          p.Status,
		"vehicle_plate":   p.VehiclePlate,
	}
	log.Infof("saving parking state: %v", state)
	if err := w.Redis.HMSet(ctx, id, state).Err(); err != nil {
		return fmt.Errorf("failed to save parking state to Redis: %s", err)
	}
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func SendParkingToStorage(p *Parking, url, token string) error {
	log.Infof("sending parking to storage: %v", p)
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %s", err)
	}

	req, err := http.NewRequest("POST", url+"/parkings", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	start := time.Now()
	defer func() { StorageLatency.Observe(time.Since(start).Seconds()) }()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("response status code %d", resp.StatusCode)
	}
	return nil
}

func (w *Worker) ProcessEntrance(m amqp091.Delivery) error {
	processingStartTs := time.Now()
	defer func() { ProcessingLatency.Observe(time.Since(processingStartTs).Seconds()) }()
	entrance := shared.Entrance{}
	if err := json.Unmarshal(m.Body, &entrance); err != nil {
		return err
	}

	ts, err := time.Parse(time.RFC3339, entrance.Ts)
	if err != nil {
		return fmt.Errorf("failed to parse entrance message date time: %s", err)
	}
	QueueingLatency.Observe(time.Since(ts).Seconds())
	defer func() { ProcessingLatency.Observe(time.Since(ts).Seconds()) }()

	log.Debugf("(worker %d) processing entrance: %v", w.Id, entrance)
	ctx := context.Background()
	lock, err := w.Locker.Obtain(ctx, "LOCK"+entrance.ParkingId, 100*time.Millisecond, w.LockOpts)
	defer lock.Release(ctx)
	if err != nil {
		return fmt.Errorf("failed to obtain lock: %s", err)
	}

	p, err := w.GetParking(entrance.ParkingId)
	if err != nil {
		return fmt.Errorf("failed to get parking state: %s", err)
	}

	if p.Status == "in_storage" {
		return errors.New("parking already stored")
	}
	if !p.EntryDateTime.IsZero() {
		return errors.New("parking already started")
	}

	entryDateTime, err := time.Parse(time.RFC3339, entrance.EntryDateTime)
	if err != nil {
		return fmt.Errorf("failed to parse entry message date time: %s", err)
	}

	if p.Status == "" {
		p.EntryDateTime = entryDateTime
		p.VehiclePlate = entrance.VehiclePlate
		p.Status = "incomplete"
	} else if !p.ExitDateTime.IsZero() {
		if entryDateTime.After(p.ExitDateTime) {
			return errors.New("entry after exit")
		}
		p.EntryDateTime = entryDateTime
		p.Status = "complete"
	}

	if err := w.SaveParking(p, entrance.ParkingId); err != nil {
		return fmt.Errorf("failed to save parking state: %s", err)
	}

	if p.Status == "complete" {
		if err := SendParkingToStorage(p, w.Settings.storageURL, w.Settings.storageToken); err != nil {
			return fmt.Errorf("failed to send parking to storage: %s", err)
		}
		p.Status = "in_storage"
		w.SaveParking(p, entrance.ParkingId)
	}
	return nil
}

func (w *Worker) ProcessExit(m amqp091.Delivery) error {
	processingStartTs := time.Now()
	defer func() { ProcessingLatency.Observe(time.Since(processingStartTs).Seconds()) }()
	exit := shared.Exit{}
	if err := json.Unmarshal(m.Body, &exit); err != nil {
		return err
	}

	ts, err := time.Parse(time.RFC3339, exit.Ts)
	if err != nil {
		return fmt.Errorf("failed to parse exit message date time: %s", err)
	}
	QueueingLatency.Observe(time.Since(ts).Seconds())

	log.Debugf("(worker %d) processing exit: %v", w.Id, exit)
	ctx := context.Background()
	lock, err := w.Locker.Obtain(ctx, "LOCK"+exit.ParkingId, 1000*time.Millisecond, w.LockOpts)
	defer lock.Release(ctx)
	if err != nil {
		return fmt.Errorf("failed to obtain lock: %s", err)
	}

	p, err := w.GetParking(exit.ParkingId)
	if err != nil {
		return fmt.Errorf("failed to get parking state: %s", err)
	}

	if p.Status == "in_storage" {
		return errors.New("parking already stored")
	}
	if !p.ExitDateTime.IsZero() {
		return errors.New("parking already ended")
	}

	exitDateTime, err := time.Parse(time.RFC3339, exit.ExitDateTime)
	if err != nil {
		return fmt.Errorf("failed to parse exit message date time: %s", err)
	}

	if p.Status == "" {
		p.ExitDateTime = exitDateTime
		p.VehiclePlate = exit.VehiclePlate
		p.Status = "incomplete"
	} else if !p.EntryDateTime.IsZero() {
		if exitDateTime.Before(p.EntryDateTime) {
			return errors.New("exit before entry")
		}
		p.ExitDateTime = exitDateTime
		p.Status = "complete"
	}

	if err := w.SaveParking(p, exit.ParkingId); err != nil {
		return fmt.Errorf("failed to save parking state: %s", err)
	}

	if p.Status == "complete" {
		if err := SendParkingToStorage(p, w.Settings.storageURL, w.Settings.storageToken); err != nil {
			return fmt.Errorf("failed to send parking to storage: %s", err)
		}
		p.Status = "in_storage"
		w.SaveParking(p, exit.ParkingId)
	}
	return nil
}

func (w *Worker) ProcessEntrances(msgs <-chan amqp091.Delivery) {
	for m := range msgs {
		if err := w.ProcessEntrance(m); err != nil {
			log.Errorf("failed to process entrance: %s", err)
			m.Nack(false, false)
		} else {
			m.Ack(false)
		}
	}
}

func (w *Worker) ProcessExits(msgs <-chan amqp091.Delivery) {
	for m := range msgs {
		if err := w.ProcessExit(m); err != nil {
			log.Errorf("failed to process exit: %s", err)
			m.Nack(false, false)
		} else {
			m.Ack(false)
		}
	}
}

func main() {
	shared.InitLog()
	settings, err := newSettings()
	shared.PanicOnError(err, "failed to read settings")

	http.Handle(settings.promPath, promhttp.Handler())
	go http.ListenAndServe(fmt.Sprintf(":%d", settings.promPort), nil)
	fmt.Printf("prometheus metrics available at http://localhost:%d%s\n", settings.promPort, settings.promPath)

	opt, err := redis.ParseURL(settings.redisURL)
	shared.PanicOnError(err, "failed to parse redis URL")
	rds := redis.NewClient(opt)
	defer rds.Close()

	locker := redislock.New(rds)
	lockOpts := &redislock.Options{
		RetryStrategy: redislock.LimitRetry(redislock.LinearBackoff(100*time.Millisecond), 3),
	}

	entr, err := shared.NewRMQueue(settings.rmqURL, settings.entrancesQueueName)
	shared.PanicOnError(err, "failed to connect to RMQ")
	defer entr.Close()

	entrances, err := entr.Consume()
	shared.PanicOnError(err, "failed to consume entrances")
	for i := 0; i < settings.entriesWorkers; i++ {
		worker := Worker{rds, locker, lockOpts, i, &settings}
		go worker.ProcessEntrances(entrances)
	}

	ext, err := shared.NewRMQueue(settings.rmqURL, settings.exitsQueueName)
	shared.PanicOnError(err, "failed to connect to RMQ")
	defer ext.Close()

	exits, err := ext.Consume()
	shared.PanicOnError(err, "failed to consume exits")
	for i := 0; i < settings.exitWorkers; i++ {
		worker := Worker{rds, locker, lockOpts, i, &settings}
		go worker.ProcessExits(exits)
	}

	select {}
}
