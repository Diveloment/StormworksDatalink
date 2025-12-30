package main

type VesselType int32

const (
	Installation VesselType = 1
	Vessel       VesselType = 2
	Missile      VesselType = 3
	Air          VesselType = 4
)

func (e VesselType) ToInt32() int32 {
	return int32(e)
}

func VesselTypeFromInt32(code int32) VesselType {
	return VesselType(code)
}
