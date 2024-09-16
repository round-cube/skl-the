package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/joho/godotenv/autoload"
	shared "github.com/round-cube/parking-meter/shared"
	log "github.com/sirupsen/logrus"
)

type Settings struct {
	RMQURL      string
	QueueName   string
	FixturePath string
}

type UTCFormatter struct {
	log.Formatter
}

func (u UTCFormatter) Format(e *log.Entry) ([]byte, error) {
	e.Time = e.Time.UTC()
	return u.Formatter.Format(e)
}

func main() {
	log.SetFormatter(UTCFormatter{&log.TextFormatter{DisableColors: true}})
	settings, err := loadSettings()
	handleError(err, "failed to read settings")

	rmq, err := shared.NewRMQueue(settings.RMQURL, settings.QueueName)
	handleError(err, "failed to connect to RMQ")
	defer rmq.Close()

	entrances, err := readEntrances(settings.FixturePath)
	handleError(err, "failed to read fixture")

	processEntrances(rmq, entrances)
}

func loadSettings() (Settings, error) {
	var s Settings
	var err error

	s.RMQURL, err = getEnv("RMQ_URL")
	if err != nil {
		return s, err
	}

	s.FixturePath, err = getEnv("FIXTURE_PATH")
	if err != nil {
		return s, err
	}

	s.QueueName = getEnvOrDefault("QUEUE_NAME", "entrances")
	return s, nil
}

func getEnv(key string) (string, error) {
	value, set := os.LookupEnv(key)
	if !set {
		return "", fmt.Errorf("environment variable must be set: %s", key)
	}
	return value, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value, set := os.LookupEnv(key); set {
		return value
	}
	return defaultValue
}

func readEntrances(path string) ([]shared.Entrance, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var entrances []shared.Entrance
	err = json.Unmarshal(bytes, &entrances)
	return entrances, err
}

func processEntrances(rmq *shared.RMQueue, entrances []shared.Entrance) {
	parkings := make(map[string]int)

	for _, entrance := range entrances {
		entryID, err := getEntryId()
		handleError(err, "failed to generate entry id")

		parkings[entrance.VehiclePlate]++
		entrance.ParkingId = fmt.Sprintf("%s:%d", entrance.VehiclePlate, parkings[entrance.VehiclePlate])
		entrance.EntryId = entryID
		entrance.Ts = time.Now().UTC().Format(time.RFC3339)

		err = publishEntrance(rmq, entrance)
		handleError(err, "failed to publish entrance event")

		logEntrance(entrance)
	}
}

func publishEntrance(rmq *shared.RMQueue, entrance shared.Entrance) error {
	entranceBytes, err := json.Marshal(entrance)
	if err != nil {
		return err
	}
	return rmq.Publish(entranceBytes)
}

func logEntrance(entrance shared.Entrance) {
	log.WithFields(log.Fields{
		"vehicle_plate":   entrance.VehiclePlate,
		"entry_date_time": entrance.EntryDateTime,
		"entry_id":        entrance.EntryId,
		"parking_id":      entrance.ParkingId,
		"ts":              entrance.Ts,
	}).Info("new entrance")
}

func getEntryId() (string, error) {
	v7, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return "ETR:" + v7.String(), nil
}

func handleError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", err, msg)
	}
}
