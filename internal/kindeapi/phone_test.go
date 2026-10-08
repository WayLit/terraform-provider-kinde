package kindeapi

import "testing"

func TestParsePhone(t *testing.T) {
	tests := []struct {
		name         string
		number       string
		wantNational string
		wantCountry  string
		wantErr      bool
	}{
		{name: "armenia", number: "+37455251234", wantNational: "55251234", wantCountry: "am"},
		{name: "australia", number: "+61412345678", wantNational: "412345678", wantCountry: "au"},
		{name: "united states", number: "+12025550123", wantNational: "2025550123", wantCountry: "us"},
		{name: "united kingdom", number: "+442079460123", wantNational: "2079460123", wantCountry: "gb"},
		{name: "too short", number: "+1234", wantErr: true},
		{name: "no country code", number: "0412345678", wantErr: true},
		{name: "not a number", number: "phone", wantErr: true},
		{name: "empty", number: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			national, country, err := parsePhone(tt.number)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePhone(%q) = %q, %q; want an error", tt.number, national, country)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if national != tt.wantNational || country != tt.wantCountry {
				t.Fatalf("parsePhone(%q) = %q, %q; want %q, %q", tt.number, national, country, tt.wantNational, tt.wantCountry)
			}
		})
	}
}
