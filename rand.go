package main

import (
	"math/rand"
	"time"
)

func GenNormalValue(mean float64, stddev float64) float64 {
	return rand.NormFloat64()*stddev + mean
}

func GenRandomDuration(mean time.Duration, stddev time.Duration) time.Duration {
	return time.Duration(GenNormalValue(float64(mean), float64(stddev)))
}

func Likelyhood(percentage float64) bool {
	return rand.Float64() < percentage
}
