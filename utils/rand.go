package utils

import (
	"math/rand"
	"time"
)

func NormalValue(mean float64, stddev float64) float64 {
	return rand.NormFloat64()*stddev + mean
}

func RandomDuration(mean time.Duration, stddev time.Duration) time.Duration {
	return time.Duration(NormalValue(float64(mean), float64(stddev)))
}

func Likelyhood(percentage float64) bool {
	return rand.Float64() < percentage
}
