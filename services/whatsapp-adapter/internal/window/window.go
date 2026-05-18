package window

import "time"

const ServiceWindow = 24 * time.Hour

func Until(lastInboundAt time.Time) time.Time {
	if lastInboundAt.IsZero() {
		return time.Time{}
	}
	return lastInboundAt.Add(ServiceWindow)
}

func IsOpen(now, lastInboundAt time.Time) bool {
	if lastInboundAt.IsZero() {
		return false
	}
	return !now.After(Until(lastInboundAt))
}
