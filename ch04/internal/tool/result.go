package tool

import "encoding/json"

type Result struct {
	Tool          string         `json:"tool"`
	CallID        string         `json:"call_id"`
	OK            bool           `json:"ok"`
	Summary       string         `json:"summary"`
	Data          map[string]any `json:"data,omitempty"`
	Error         *ToolError     `json:"error,omitempty"`
	Truncated     bool           `json:"truncated"`
	OriginalBytes int            `json:"original_bytes,omitempty"`
	ReturnedBytes int            `json:"returned_bytes,omitempty"`
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Success(name, callID, summary string, data map[string]any) Result {
	return Result{Tool: name, CallID: callID, OK: true, Summary: summary, Data: data}
}

func Failure(name, callID, code, message string) Result {
	return Result{
		Tool:    name,
		CallID:  callID,
		OK:      false,
		Summary: message,
		Error:   &ToolError{Code: code, Message: message},
	}
}

func (r Result) JSON() string {
	data, err := json.Marshal(r)
	if err != nil {
		fallback, _ := json.Marshal(Failure(r.Tool, r.CallID, "result_encoding_failed", err.Error()))
		return string(fallback)
	}
	return string(data)
}
