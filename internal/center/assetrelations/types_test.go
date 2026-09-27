package assetrelations

import "testing"

func TestAssociationInputsKeepPlacementSeparate(t *testing.T) {
	port := 443
	service := "s1"
	for _, tt := range []struct {
		kind  string
		input LinkInput
		valid bool
	}{
		{Service, LinkInput{ObjectID: "s1", Port: &port}, true},
		{Domain, LinkInput{ObjectID: "d1", ServiceID: &service}, true},
		{Domain, LinkInput{ObjectID: "d1", Port: &port}, false},
		{Service, LinkInput{ObjectID: "s1", ServiceID: &service}, false},
		{Service, LinkInput{}, false},
		{"unknown", LinkInput{ObjectID: "s1"}, false},
	} {
		if got := Validate(tt.kind, tt.input) == nil; got != tt.valid {
			t.Errorf("Validate(%s,%+v)=%v", tt.kind, tt.input, got)
		}
	}
}
