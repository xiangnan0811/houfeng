package activity

import "encoding/json"

// marshalWire exists so Event.MarshalJSON can encode its alias type without
// recursing back into the custom marshaller.
func marshalWire(value any) ([]byte, error) {
	return json.Marshal(value)
}
