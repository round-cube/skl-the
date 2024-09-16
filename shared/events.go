package shared

type Entrance struct {
	VehiclePlate  string `json:"vehicle_plate"`
	EntryDateTime string `json:"entry_date_time"`
	EntryId       string `json:"entry_id"`
	ParkingId     string `json:"parking_id"`
	Ts            string `json:"ts"`
}

type Exit struct {
	VehiclePlate string `json:"vehicle_plate"`
	ExitDateTime string `json:"exit_date_time"`
	EntryId      string `json:"entry_id"`
	ParkingId    string `json:"parking_id"`
	Ts           string `json:"ts"`
}
