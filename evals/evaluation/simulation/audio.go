package simulation

import (
	"encoding/binary"
	"errors"
	"log"
	"regexp"
	"strconv"
)

// Live API audio rates for 16-bit mono PCM: speech goes in at 16 kHz, and
// text-to-speech backends commonly produce 24 kHz.
const (
	liveInputRateHz  = 16000
	liveOutputRateHz = 24000
	// LiveInputMIMEType is the MIME type of audio sent to a live session.
	LiveInputMIMEType = "audio/pcm;rate=16000"
)

// rateRE matches a whole rate= parameter, so bitrate= does not count.
var rateRE = regexp.MustCompile(`(?i)(?:^|;)\s*rate\s*=\s*(\d+)\s*(?:;|$)`)

// ParseSampleRate returns the rate= of a MIME type such as
// "audio/pcm;rate=24000", or def when it has none.
func ParseSampleRate(mimeType string, def int) int {
	m := rateRE.FindStringSubmatch(mimeType)
	if m == nil {
		return def
	}
	rate, err := strconv.Atoi(m[1])
	if err != nil {
		return def
	}
	return rate
}

// ResamplePCM16 resamples little-endian 16-bit mono PCM by linear
// interpolation, as adk-python's resample_pcm16 does: plenty for speech fed
// to a transcribing model. Equal rates, or input of fewer than two samples,
// come back unchanged; a trailing odd byte is dropped.
func ResamplePCM16(pcm []byte, srcRate, dstRate int) ([]byte, error) {
	if srcRate <= 0 || dstRate <= 0 {
		return nil, errors.New("Sample rates must be positive")
	}
	if len(pcm) == 0 || srcRate == dstRate {
		return pcm, nil
	}
	n := len(pcm) / 2
	if n < 2 {
		return pcm, nil
	}
	sample := func(i int) float64 { return float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) }
	ratio := float64(srcRate) / float64(dstRate)
	outLen := max(1, int(float64(n)/ratio))
	out := make([]byte, 2*outLen)
	for i := range outLen {
		pos := float64(i) * ratio
		left := int(pos)
		right := min(left+1, n-1)
		frac := pos - float64(left)
		// int() truncates toward zero, matching Python's int().
		v := int16(sample(left)*(1-frac) + sample(right)*frac)
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out, nil
}

// ToLiveInput resamples synthesized speech to the 16 kHz a live session
// takes. A MIME type without rate= is assumed to be 24 kHz, with a log line,
// since a wrong guess mis-pitches the audio silently otherwise.
func ToLiveInput(pcm []byte, sourceMIMEType string) ([]byte, error) {
	if !rateRE.MatchString(sourceMIMEType) {
		log.Printf("evals: audio mime type %q has no rate=; assuming %d Hz before resampling to the live input rate", sourceMIMEType, liveOutputRateHz)
	}
	return ResamplePCM16(pcm, ParseSampleRate(sourceMIMEType, liveOutputRateHz), liveInputRateHz)
}
