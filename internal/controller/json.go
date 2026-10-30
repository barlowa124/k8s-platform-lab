package controller

import "encoding/json"

func toJSON(v any) ([]byte, error) { return json.Marshal(v) }
