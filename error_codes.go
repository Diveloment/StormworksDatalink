package main

type ErrorCode int32

const (
	ErrorSuccess         ErrorCode = -1
	ErrorWrongID         ErrorCode = 1201
	ErrorWrongVesselType ErrorCode = 1202
	ErrorUnauthorized    ErrorCode = 12401
	ErrorNotFound        ErrorCode = 12404
	ErrorServerError     ErrorCode = 12500
)

func (e ErrorCode) ToInt32() int32 {
	return int32(e)
}
