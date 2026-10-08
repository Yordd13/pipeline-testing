// TestDecodesAClassAPositionReport: checks that a Class A report decodes MMSI, time, trimmed name and all readings.
// TestNotAvailableSentinelsBecomeAbsentRatherThanReadings: checks that not-available speed, course and heading become nil.
// TestAnExtendedClassBReportNamesItsOwnVessel: checks that a message 19 report yields its own trimmed name and ship type.
// TestPositionFallsBackToMetaDataWhenTheBodyHasNone: checks that coordinates come from MetaData when the body lacks them.
// TestPositionReportsThatAreDropped: checks that invalid, out-of-range, mis-keyed or MMSI-less position frames are dropped.
// TestStaticMessagesContributeNameAndShipType: checks that ship static data and Class B parts A and B yield name and type.
// TestAStaticMessageThatSaysNothingIsDropped: checks that static messages with no usable name or type are dropped.
// TestNonAISFramesAreIgnoredRatherThanFailedOn: checks that confirmations, unwanted types and non-JSON frames are ignored.
// TestAMessageWithNoTimeIsStampedOnArrivalAndSaysSo: checks that a missing or bad time_utc uses arrival time and flags it.
// TestUsableReadings: checks which speed, course, heading and ship type values are kept or discarded.
// TestTrimAISText: checks that AIS padding is trimmed and that firstNonEmpty skips blank candidates.
// TestSubscriptionIsLatitudeFirstAndThreeLevelsDeep: checks that the subscription box is one latitude-first corner pair.
// TestTheKeyNeverAppearsInTheRedactedFrame: checks that the redacted frame hides the key while the sent frame carries it.

package aiscollect

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var arrived = time.Date(2026, 9, 22, 15, 59, 30, 0, time.UTC)

func TestDecodesAClassAPositionReport(t *testing.T) {
	raw := `{
	  "MessageType": "PositionReport",
	  "MetaData": {
	    "MMSI": 207400000,
	    "MMSI_String": 207400000,
	    "ShipName": "EXAMPLE VESSEL   ",
	    "latitude": 43.1234,
	    "longitude": 28.5678,
	    "time_utc": "2026-09-22 15:59:20.945924 +0000 UTC"
	  },
	  "Message": {
	    "PositionReport": {
	      "MessageID": 1, "UserID": 207400000, "Valid": true,
	      "NavigationalStatus": 0, "Sog": 11.4, "Cog": 187.2,
	      "TrueHeading": 186, "Latitude": 43.1234, "Longitude": 28.5678
	    }
	  }
	}`

	message, ok := decodeMessage([]byte(raw), arrived)
	if !ok || message.Position == nil {
		t.Fatal("a position report was not decoded")
	}

	position := *message.Position
	if position.MMSI != "207400000" {
		t.Errorf("MMSI is %q", position.MMSI)
	}
	if position.Timestamp != "2026-09-22T15:59:20.945924Z" {
		t.Errorf("timestamp is %q; time_utc is a Go time rendered by String, not RFC3339",
			position.Timestamp)
	}
	if message.ClockFromArrival {
		t.Error("the message carried a usable time_utc and should not have been stamped on arrival")
	}
	if position.Name != "EXAMPLE VESSEL" {
		t.Errorf("name is %q; AIS pads names with spaces and @", position.Name)
	}
	if position.NavStatus == nil || *position.NavStatus != 0 {
		t.Error("navigational status 0 is under way using engine, a real reading, and must survive")
	}
	if position.Heading == nil || *position.Heading != 186 {
		t.Error("heading was lost")
	}
	if position.SOG == nil || *position.SOG != 11.4 || position.COG == nil || *position.COG != 187.2 {
		t.Error("speed or course was lost")
	}
}

func TestNotAvailableSentinelsBecomeAbsentRatherThanReadings(t *testing.T) {
	raw := `{
	  "MessageType": "StandardClassBPositionReport",
	  "MetaData": {"MMSI": 111111111, "time_utc": "2026-09-22 15:59:20 +0000 UTC"},
	  "Message": {"StandardClassBPositionReport": {
	    "UserID": 111111111, "Valid": true,
	    "Sog": 102.3, "Cog": 360, "TrueHeading": 511,
	    "Latitude": 43.5, "Longitude": 28.5
	  }}
	}`

	message, ok := decodeMessage([]byte(raw), arrived)
	if !ok || message.Position == nil {
		t.Fatal("a Class B position report was not decoded")
	}
	position := *message.Position

	if position.SOG != nil {
		t.Errorf("speed 102.3 means not available and must not be stored as %v", *position.SOG)
	}
	if position.COG != nil {
		t.Errorf("course 360 means not available and must not be stored as %v", *position.COG)
	}
	if position.Heading != nil {
		t.Errorf("heading 511 means not available and must not be stored as %v", *position.Heading)
	}
	if position.NavStatus != nil {
		t.Error("Class B carries no navigational status; an absent field must stay absent")
	}
}

func TestAnExtendedClassBReportNamesItsOwnVessel(t *testing.T) {
	raw := `{
	  "MessageType": "ExtendedClassBPositionReport",
	  "MetaData": {"MMSI": 222222222, "time_utc": "2026-09-22T15:59:20Z"},
	  "Message": {"ExtendedClassBPositionReport": {
	    "UserID": 222222222, "Valid": true, "Latitude": 43.2, "Longitude": 28.1,
	    "Name": "  SAILOR@@@@", "Type": 36
	  }}
	}`

	message, ok := decodeMessage([]byte(raw), arrived)
	if !ok || message.Position == nil {
		t.Fatal("an extended Class B report was not decoded")
	}
	if message.Position.Name != "SAILOR" {
		t.Errorf("name is %q, want SAILOR", message.Position.Name)
	}
	if message.Position.ShipType == nil || *message.Position.ShipType != 36 {
		t.Error("ship type 36 (sailing) was lost")
	}
	if message.Position.Timestamp != "2026-09-22T15:59:20Z" {
		t.Errorf("an RFC3339 time_utc should be taken as is, got %q", message.Position.Timestamp)
	}
}

func TestPositionFallsBackToMetaDataWhenTheBodyHasNone(t *testing.T) {
	raw := `{
	  "MessageType": "PositionReport",
	  "MetaData": {"MMSI": 111111111, "latitude": 42.5, "longitude": 27.9,
	               "time_utc": "2026-09-22 15:59:20 +0000 UTC"},
	  "Message": {"PositionReport": {"UserID": 111111111, "Valid": true}}
	}`

	message, ok := decodeMessage([]byte(raw), arrived)
	if !ok {
		t.Fatal("the position in MetaData was not used")
	}
	if message.Position.Latitude != 42.5 || message.Position.Longitude != 27.9 {
		t.Errorf("position is %v,%v, want the MetaData copy 42.5,27.9",
			message.Position.Latitude, message.Position.Longitude)
	}
}

func TestPositionReportsThatAreDropped(t *testing.T) {
	position := func(body string) string {
		return `{"MessageType":"PositionReport",
		  "MetaData":{"MMSI":111111111,"time_utc":"2026-09-22 15:59:20 +0000 UTC"},
		  "Message":{"PositionReport":` + body + `}}`
	}
	tests := []struct {
		name string
		raw  string
	}{
		{"not-available sentinels", position(`{"Valid":true,"Latitude":91,"Longitude":181}`)},
		{"latitude out of range", position(`{"Valid":true,"Latitude":-95,"Longitude":28}`)},
		{"longitude out of range", position(`{"Valid":true,"Latitude":43,"Longitude":200}`)},
		{"null island", position(`{"Valid":true,"Latitude":0,"Longitude":0}`)},
		{"flagged invalid", position(`{"Valid":false,"Latitude":43,"Longitude":28}`)},
		{"body is not an object", position(`"nonsense"`)},
		{"body keyed by another type", `{"MessageType":"PositionReport","MetaData":{"MMSI":1},
		  "Message":{"StandardClassBPositionReport":{"Latitude":43,"Longitude":28}}}`},
		{"Message is not an object", `{"MessageType":"PositionReport","MetaData":{"MMSI":1},
		  "Message":[1,2]}`},
		{"no MMSI", `{"MessageType":"PositionReport","MetaData":{},
		  "Message":{"PositionReport":{"Latitude":43,"Longitude":28}}}`},
		{"MMSI zero", `{"MessageType":"PositionReport","MetaData":{"MMSI":0},
		  "Message":{"PositionReport":{"Latitude":43,"Longitude":28}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if message, ok := decodeMessage([]byte(test.raw), arrived); ok {
				t.Fatalf("decoded %+v; the frame should have been dropped", message)
			}
		})
	}
}

func TestStaticMessagesContributeNameAndShipType(t *testing.T) {
	shipStatic := `{
	  "MessageType": "ShipStaticData",
	  "MetaData": {"MMSI": 207400000},
	  "Message": {"ShipStaticData": {
	    "UserID": 207400000, "Valid": true, "Name": "EXAMPLE VESSEL@@@", "Type": 70
	  }}
	}`

	message, ok := decodeMessage([]byte(shipStatic), arrived)
	if !ok || message.Identity == nil {
		t.Fatal("a static data message was not decoded")
	}
	if message.Identity.Name != "EXAMPLE VESSEL" {
		t.Errorf("name is %q", message.Identity.Name)
	}
	if message.Identity.ShipType == nil || *message.Identity.ShipType != 70 {
		t.Error("ship type 70 (cargo) was lost")
	}

	partB := `{
	  "MessageType": "StaticDataReport",
	  "MetaData": {"MMSI": 111111111},
	  "Message": {"StaticDataReport": {
	    "UserID": 111111111, "Valid": true, "PartNumber": true,
	    "ReportA": {"Valid": false, "Name": ""},
	    "ReportB": {"Valid": true, "ShipType": 37}
	  }}
	}`

	message, ok = decodeMessage([]byte(partB), arrived)
	if !ok || message.Identity == nil {
		t.Fatal("a Class B static report was not decoded")
	}
	if message.Identity.ShipType == nil || *message.Identity.ShipType != 37 {
		t.Error("ship type 37 (pleasure craft) was lost from part B")
	}

	partA := `{
	  "MessageType": "StaticDataReport",
	  "MetaData": {"MMSI": 111111111},
	  "Message": {"StaticDataReport": {"ReportA": {"Valid": true, "Name": "PART A "}}}
	}`
	message, ok = decodeMessage([]byte(partA), arrived)
	if !ok || message.Identity == nil || message.Identity.Name != "PART A" {
		t.Fatalf("the name in part A was lost: %+v", message.Identity)
	}
}

func TestAStaticMessageThatSaysNothingIsDropped(t *testing.T) {
	tests := map[string]string{
		"no name and type unavailable": `{"MessageType":"ShipStaticData","MetaData":{"MMSI":1},
		  "Message":{"ShipStaticData":{"Name":"@@@@","Type":0}}}`,
		"part B not valid": `{"MessageType":"StaticDataReport","MetaData":{"MMSI":1},
		  "Message":{"StaticDataReport":{"ReportB":{"Valid":false,"ShipType":37}}}}`,
		"wrong body": `{"MessageType":"ShipStaticData","MetaData":{"MMSI":1},
		  "Message":{"StaticDataReport":{"Name":"X"}}}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := decodeMessage([]byte(raw), arrived); ok {
				t.Fatal("a static message with nothing usable was decoded")
			}
		})
	}
}

func TestNonAISFramesAreIgnoredRatherThanFailedOn(t *testing.T) {
	confirmation := `{"MessageType":"SubscriptionConfirmation","Message":{"CompressionEnabled":true}}`
	if _, ok := decodeMessage([]byte(confirmation), arrived); ok {
		t.Error("the subscription confirmation was decoded as an AIS message")
	}

	base := `{"MessageType":"BaseStationReport","MetaData":{"MMSI":2070000},
	          "Message":{"BaseStationReport":{"UserID":2070000}}}`
	if _, ok := decodeMessage([]byte(base), arrived); ok {
		t.Error("a base station report was stored as a vessel")
	}

	if _, ok := decodeMessage([]byte("not json at all"), arrived); ok {
		t.Error("an unparseable frame was decoded")
	}
}

func TestAMessageWithNoTimeIsStampedOnArrivalAndSaysSo(t *testing.T) {
	for _, timeUTC := range []string{"", "yesterday afternoon"} {
		raw := `{
		  "MessageType": "PositionReport",
		  "MetaData": {"MMSI": 111111111, "time_utc": "` + timeUTC + `"},
		  "Message": {"PositionReport": {
		    "UserID": 111111111, "Valid": true, "Latitude": 43.5, "Longitude": 28.5
		  }}
		}`

		message, ok := decodeMessage([]byte(raw), arrived)
		if !ok {
			t.Fatalf("time_utc %q: the message was dropped instead of being stamped on arrival", timeUTC)
		}
		if !message.ClockFromArrival {
			t.Fatalf("time_utc %q: a position stamped with its arrival time must be counted as such", timeUTC)
		}
		if message.Position.Timestamp != arrived.Format(time.RFC3339Nano) {
			t.Errorf("timestamp is %q, want the arrival time", message.Position.Timestamp)
		}
	}
}

func TestUsableReadings(t *testing.T) {
	float := func(v float64) *float64 { return &v }
	integer := func(v int) *int { return &v }

	floats := []struct {
		name  string
		check func(*float64) *float64
		in    *float64
		kept  bool
	}{
		{"speed absent", usableSpeed, nil, false},
		{"speed at rest", usableSpeed, float(0), true},
		{"speed negative", usableSpeed, float(-1), false},
		{"speed unavailable", usableSpeed, float(102.3), false},
		{"course due north", usableCourse, float(0), true},
		{"course negative", usableCourse, float(-0.1), false},
		{"course unavailable", usableCourse, float(360), false},
	}
	for _, test := range floats {
		if got := test.check(test.in); (got != nil) != test.kept {
			t.Errorf("%s: kept=%v, want %v", test.name, got != nil, test.kept)
		}
	}

	ints := []struct {
		name  string
		check func(*int) *int
		in    *int
		kept  bool
	}{
		{"heading absent", usableHeading, nil, false},
		{"heading north", usableHeading, integer(0), true},
		{"heading negative", usableHeading, integer(-1), false},
		{"heading unavailable", usableHeading, integer(511), false},
		{"ship type absent", usableShipType, nil, false},
		{"ship type unavailable", usableShipType, integer(0), false},
		{"ship type cargo", usableShipType, integer(70), true},
	}
	for _, test := range ints {
		if got := test.check(test.in); (got != nil) != test.kept {
			t.Errorf("%s: kept=%v, want %v", test.name, got != nil, test.kept)
		}
	}
}

func TestTrimAISText(t *testing.T) {
	tests := map[string]string{
		"":                   "",
		"@@@@":               "",
		"   ":                "",
		"  NAME  @@@":        "NAME",
		"TWO WORDS":          "TWO WORDS",
		"KEEPS @ IN MIDDLE@": "KEEPS @ IN MIDDLE",
	}
	for in, want := range tests {
		if got := trimAISText(in); got != want {
			t.Errorf("trimAISText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := firstNonEmpty("@@", "  ", "THIRD", "FOURTH"); got != "THIRD" {
		t.Errorf("firstNonEmpty skipped to %q, want THIRD", got)
	}
}

func TestSubscriptionIsLatitudeFirstAndThreeLevelsDeep(t *testing.T) {
	if len(bulgarianWatersBox) != 1 || len(bulgarianWatersBox[0]) != 2 {
		t.Fatalf("the box is %v; the documentation wants one box of two corners",
			bulgarianWatersBox)
	}
	southWest, northEast := bulgarianWatersBox[0][0], bulgarianWatersBox[0][1]
	if southWest[0] != 41.8 || southWest[1] != 27.2 {
		t.Errorf("south-west corner is %v, want [41.8 27.2] as [lat lon]", southWest)
	}
	if northEast[0] != 44.0 || northEast[1] != 30.5 {
		t.Errorf("north-east corner is %v, want [44 30.5] as [lat lon]", northEast)
	}
}

func TestTheKeyNeverAppearsInTheRedactedFrame(t *testing.T) {
	secret := "not-a-real-key-0123456789"
	config := NewDefaultConfig()
	feed := &stream{config: &config, apiKey: secret}

	frame := feed.redactedSubscription()
	if strings.Contains(frame, secret) {
		t.Fatal("the redacted subscription frame contains the key")
	}
	if !strings.Contains(frame, "<"+config.APIKeyEnv+", not shown>") {
		t.Errorf("the redacted frame should name the variable the key came from, unescaped:\n%s", frame)
	}

	sent, err := json.Marshal(feed.subscriptionFrame())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sent), secret) {
		t.Error("the subscription actually sent does not carry the key")
	}
	if !strings.Contains(string(sent), "ShipStaticData") {
		t.Error("the subscription does not filter message types")
	}
}
