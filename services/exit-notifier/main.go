package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	_ "github.com/joho/godotenv/autoload"
	shared "github.com/round-cube/parking-meter/shared"
	log "github.com/sirupsen/logrus"
)

const (
	letters = "ABCDEFHIJKLMNOPQRVXYZ"
	digits  = "0123456789"
)

type Settings struct {
	RMQURL            string
	QueueName         string
	FixturePath       string
	ParkingSecondsMin int
	ParkingSecondsMax int
	MatchRate         float64
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
	panicOnError(err, "failed to read settings")

	rmq, err := shared.NewRMQueue(settings.RMQURL, settings.QueueName)
	panicOnError(err, "failed to connect to RMQ")
	defer rmq.Close()

	entrances, err := readEntrances(settings.FixturePath)
	panicOnError(err, "failed to read fixture")

	parkings := make(map[string]int)
	processEntrances(settings, rmq, entrances, parkings)
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
	s.QueueName = getEnvOrDefault("QUEUE_NAME", "exits")
	s.ParkingSecondsMin = getEnvOrDefaultInt("PARKING_SECONDS_MIN", 10000)
	s.ParkingSecondsMax = getEnvOrDefaultInt("PARKING_SECONDS_MAX", 100000)
	s.MatchRate, err = getEnvOrDefaultFloat("MATCH_RATE", 0.8)
	if err != nil {
		return s, err
	}
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

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value, set := os.LookupEnv(key); set {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvOrDefaultFloat(key string, defaultValue float64) (float64, error) {
	if value, set := os.LookupEnv(key); set {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			if floatValue < 0 || floatValue > 1 {
				return 0, errors.New("MATCH_RATE must be between 0 and 1")
			}
			return floatValue, nil
		}
	}
	return defaultValue, nil
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
	if err := json.Unmarshal(bytes, &entrances); err != nil {
		return nil, err
	}
	return entrances, nil
}

func processEntrances(s Settings, rmq *shared.RMQueue, entrances []shared.Entrance, parkings map[string]int) {
	for _, entrance := range entrances {
		pushEntrance(s, rmq, entrance, parkings)
	}
	for i := 0; i < len(entrances)*int(1-s.MatchRate); i++ {
		entrance := shared.Entrance{
			VehiclePlate:  getVehiclePlate(parkings),
			EntryDateTime: time.Now().UTC().Format(time.RFC3339),
		}
		pushEntrance(s, rmq, entrance, parkings)
	}
}

func pushEntrance(s Settings, rmq *shared.RMQueue, entrance shared.Entrance, parkings map[string]int) {
	entryDateTime, err := time.Parse(time.RFC3339, entrance.EntryDateTime)
	panicOnError(err, "failed to parse entry date time")

	exitDateTime := entryDateTime.Add(time.Duration(rand.Intn(s.ParkingSecondsMax-s.ParkingSecondsMin)+s.ParkingSecondsMin) * time.Second).Format(time.RFC3339)
	entryID, err := getEntryId()
	panicOnError(err, "failed to generate entry id")

	parkings[entrance.VehiclePlate]++
	parkingID := fmt.Sprintf("%s:%d", entrance.VehiclePlate, parkings[entrance.VehiclePlate])
	ts := time.Now().UTC().Format(time.RFC3339)

	exitEvent := shared.Exit{
		VehiclePlate: entrance.VehiclePlate,
		ExitDateTime: exitDateTime,
		EntryId:      entryID,
		Ts:           ts,
		ParkingId:    parkingID,
	}

	entranceBytes, err := json.Marshal(exitEvent)
	panicOnError(err, "failed to encode entrance event")
	err = rmq.Publish(entranceBytes)
	panicOnError(err, "failed to publish entrance event")

	log.WithFields(log.Fields{
		"vehicle_plate":  exitEvent.VehiclePlate,
		"exit_date_time": exitEvent.ExitDateTime,
		"entry_id":       exitEvent.EntryId,
		"parking_id":     exitEvent.ParkingId,
		"ts":             ts,
	}).Info("new exit")
}

func getEntryId() (string, error) {
	v7, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return "EXT:" + v7.String(), nil
}

func getVehiclePlate(parkings map[string]int) string {
	for {
		letterPart := make([]byte, 3)
		for i := range letterPart {
			letterPart[i] = letters[rand.Intn(len(letters))]
		}

		digitPart := make([]byte, 3)
		for i := range digitPart {
			digitPart[i] = digits[rand.Intn(len(digits))]
		}

		plate := fmt.Sprintf("%s-%s", string(letterPart), string(digitPart))
		if parkings[plate] == 0 {
			return plate
		}
	}
}

func panicOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", err, msg)
	}
}
