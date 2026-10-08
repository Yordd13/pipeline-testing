// decodeMessage: parses one socket frame into a position or identity, ignoring non-AIS frames and unknown types.
// decodePosition: builds a stored record from a position report, dropping invalid or unavailable positions.
// decodeStatic: extracts a vessel's name and ship type from a static message, dropping it if both are missing.
// unwrapBody: decodes the typed body stored under the MessageType key of the Message object.
// mmsiOf: returns the MMSI from the message metadata, or an empty string when it is missing or zero.
// positionOf: picks the body's coordinates, else the metadata's, rejecting unavailable, out-of-range or 0,0 positions.
// momentOf: parses time_utc in RFC3339 or Go String layout, falling back to the arrival time and flagging it.
// usableSpeed: returns the speed over ground, or nil when it is absent, negative or the not-available value.
// usableCourse: returns the course over ground, or nil when it is absent, negative or 360 and above.
// usableHeading: returns the true heading, or nil when it is absent, negative or the 511 not-available value.
// usableShipType: returns the ship type, or nil when it is absent or zero.
// firstNonEmpty: returns the first candidate that is non-empty after trimming AIS padding.
// trimAISText: strips trailing @ and space padding and leading spaces from an AIS text field.

package main

import (
	"encoding/json"
	"errors"
	"time"

)

type envelope struct {
	MessageType string          `json:"MessageType"`
	MetaData    metaData        `json:"MetaData"`
	Message     json.RawMessage `json:"Message"`
}

type metaData struct {
	MMSI      json.Number `json:"MMSI"`
	ShipName  string      `json:"ShipName"`
	Latitude  float64     `json:"latitude"`
	Longitude float64     `json:"longitude"`
	TimeUTC   string      `json:"time_utc"`
}

type positionBody struct {
	UserID             *int64   `json:"UserID"`
	Valid              *bool    `json:"Valid"`
	Latitude           *float64 `json:"Latitude"`
	Longitude          *float64 `json:"Longitude"`
	Sog                *float64 `json:"Sog"`
	Cog                *float64 `json:"Cog"`
	TrueHeading        *int     `json:"TrueHeading"`
	NavigationalStatus *int     `json:"NavigationalStatus"`

	Name string `json:"Name"`
	Type *int   `json:"Type"`
}

type staticBody struct {
	UserID *int64 `json:"UserID"`
	Name   string `json:"Name"`
	Type   *int   `json:"Type"`

	ReportA struct {
		Valid bool   `json:"Valid"`
		Name  string `json:"Name"`
	} `json:"ReportA"`
	ReportB struct {
		Valid    bool `json:"Valid"`
		ShipType *int `json:"ShipType"`
	} `json:"ReportB"`
}

const (
	latitudeUnavailable  = 91.0
	longitudeUnavailable = 181.0
	speedUnavailable     = 102.3
	courseUnavailable    = 360.0
	headingUnavailable   = 511
	shipTypeUnavailable  = 0
)

type vesselIdentity struct {
	Name     string
	ShipType *int
}

type decoded struct {
	Position *Record
	Identity *vesselIdentity
	MMSI     string

	ClockFromArrival bool
}

func decodeMessage(raw []byte, arrived time.Time) (decoded, bool) {
	var packet envelope
	if err := json.Unmarshal(raw, &packet); err != nil {
		return decoded{}, false
	}

	mmsi := mmsiOf(packet)
	if mmsi == "" {
		return decoded{}, false
	}

	switch packet.MessageType {
	case "PositionReport", "StandardClassBPositionReport", "ExtendedClassBPositionReport":
		return decodePosition(packet, mmsi, arrived)
	case "ShipStaticData", "StaticDataReport":
		return decodeStatic(packet, mmsi)
	default:
		return decoded{}, false
	}
}

func decodePosition(packet envelope, mmsi string, arrived time.Time) (decoded, bool) {
	var body positionBody
	if err := unwrapBody(packet, &body); err != nil {
		return decoded{}, false
	}
	if body.Valid != nil && !*body.Valid {
		return decoded{}, false
	}

	latitude, longitude, ok := positionOf(body, packet.MetaData)
	if !ok {
		return decoded{}, false
	}

	moment, fromArrival := momentOf(packet.MetaData.TimeUTC, arrived)

	record := Record{
		MMSI:      mmsi,
		Timestamp: moment.Format(time.RFC3339Nano),
		Latitude:  latitude,
		Longitude: longitude,
		SOG:       usableSpeed(body.Sog),
		COG:       usableCourse(body.Cog),
		Heading:   usableHeading(body.TrueHeading),
		NavStatus: body.NavigationalStatus,
		Name:      firstNonEmpty(packet.MetaData.ShipName, body.Name),
		ShipType:  usableShipType(body.Type),
	}
	return decoded{Position: &record, MMSI: mmsi, ClockFromArrival: fromArrival}, true
}

func decodeStatic(packet envelope, mmsi string) (decoded, bool) {
	var body staticBody
	if err := unwrapBody(packet, &body); err != nil {
		return decoded{}, false
	}

	identity := vesselIdentity{
		Name:     firstNonEmpty(body.Name, body.ReportA.Name, packet.MetaData.ShipName),
		ShipType: usableShipType(body.Type),
	}
	if identity.ShipType == nil && body.ReportB.Valid {
		identity.ShipType = usableShipType(body.ReportB.ShipType)
	}
	if identity.Name == "" && identity.ShipType == nil {
		return decoded{}, false
	}
	return decoded{Identity: &identity, MMSI: mmsi}, true
}

var errWrongBody = errors.New("the Message object does not hold the type its MessageType names")

func unwrapBody(packet envelope, into any) error {
	var byName map[string]json.RawMessage
	if err := json.Unmarshal(packet.Message, &byName); err != nil {
		return err
	}
	body, found := byName[packet.MessageType]
	if !found {
		return errWrongBody
	}
	return json.Unmarshal(body, into)
}

func mmsiOf(packet envelope) string {
	text := packet.MetaData.MMSI.String()
	if text == "" || text == "0" {
		return ""
	}
	return text
}

func positionOf(body positionBody, meta metaData) (latitude, longitude float64, ok bool) {
	latitude, longitude = meta.Latitude, meta.Longitude
	if body.Latitude != nil && body.Longitude != nil {
		latitude, longitude = *body.Latitude, *body.Longitude
	}
	if latitude == latitudeUnavailable || longitude == longitudeUnavailable {
		return 0, 0, false
	}
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return 0, 0, false
	}
	if latitude == 0 && longitude == 0 {
		return 0, 0, false
	}
	return latitude, longitude, true
}

func momentOf(text string, arrived time.Time) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
	} {
		if moment, err := time.Parse(layout, text); err == nil {
			return moment.UTC(), false
		}
	}
	return arrived.UTC(), true
}

func usableSpeed(value *float64) *float64 {
	if value == nil || *value >= speedUnavailable || *value < 0 {
		return nil
	}
	return value
}

func usableCourse(value *float64) *float64 {
	if value == nil || *value >= courseUnavailable || *value < 0 {
		return nil
	}
	return value
}

func usableHeading(value *int) *int {
	if value == nil || *value >= headingUnavailable || *value < 0 {
		return nil
	}
	return value
}

func usableShipType(value *int) *int {
	if value == nil || *value == shipTypeUnavailable {
		return nil
	}
	return value
}

func firstNonEmpty(candidates ...string) string {
	for _, candidate := range candidates {
		if trimmed := trimAISText(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trimAISText(text string) string {
	end := len(text)
	for end > 0 && (text[end-1] == '@' || text[end-1] == ' ') {
		end--
	}
	start := 0
	for start < end && text[start] == ' ' {
		start++
	}
	return text[start:end]
}
