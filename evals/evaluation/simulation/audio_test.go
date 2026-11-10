package simulation_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/simulation"
)

func pcm(samples ...int16) []byte {
	b := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(s))
	}
	return b
}

func ramp(n int) []byte {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16(i)
	}
	return pcm(s...)
}

func samplesOf(b []byte) []int16 {
	s := make([]int16, len(b)/2)
	for i := range s {
		s[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return s
}

// Mirrors adk-python test_audio_utils.py:47-69.
func TestParseSampleRate(t *testing.T) {
	for _, tc := range []struct {
		mime string
		def  int
		want int
	}{
		{"audio/l16; rate=24000", 8000, 24000},
		{"audio/pcm", 16000, 16000},
		{"", 16000, 16000},
		{"audio/pcm;bitrate=128000", 24000, 24000},
		{"audio/pcm;RATE=16000", 24000, 16000},
	} {
		if got := simulation.ParseSampleRate(tc.mime, tc.def); got != tc.want {
			t.Errorf("ParseSampleRate(%q, %d) = %d, want %d", tc.mime, tc.def, got, tc.want)
		}
	}
}

// Mirrors adk-python test_audio_utils.py:79-119.
func TestResamplePCM16(t *testing.T) {
	resample := func(in []byte, src, dst int) []byte {
		t.Helper()
		out, err := simulation.ResamplePCM16(in, src, dst)
		if err != nil {
			t.Fatalf("ResamplePCM16: %v", err)
		}
		return out
	}
	if in := pcm(1, 2, 3, 4); !bytes.Equal(resample(in, 16000, 16000), in) {
		t.Error("equal rates changed the input")
	}
	if got := resample(nil, 24000, 16000); len(got) != 0 {
		t.Errorf("empty input = %v, want empty", got)
	}
	for _, r := range [][2]int{{0, 16000}, {24000, 0}} {
		if _, err := simulation.ResamplePCM16(pcm(1, 2), r[0], r[1]); err == nil || !strings.Contains(err.Error(), "Sample rates must be positive") {
			t.Errorf("rates %v error = %v, want Sample rates must be positive", r, err)
		}
	}
	if in := pcm(42); !bytes.Equal(resample(in, 24000, 16000), in) {
		t.Error("a single sample changed")
	}
	if got := samplesOf(resample(ramp(600), 24000, 16000)); len(got) != 400 {
		t.Errorf("600 samples at 24 kHz -> %d at 16 kHz, want 400", len(got))
	}
	if got := samplesOf(resample(pcm(0, 100, 200, 300), 24000, 16000)); len(got) < 2 || got[1] != 150 {
		t.Errorf("resampled = %v, want index 1 = 150", got)
	}
}

// A trailing odd byte is dropped before resampling.
func TestResamplePCM16DropsTrailingByte(t *testing.T) {
	out, err := simulation.ResamplePCM16(append(ramp(600), 7), 24000, 16000)
	if err != nil || len(out) != 800 {
		t.Errorf("len = %d, err = %v; want 800 bytes", len(out), err)
	}
}

// Mirrors adk-python test_audio_utils.py:135-170.
func TestToLiveInput(t *testing.T) {
	for _, mime := range []string{"audio/l16; rate=24000", "audio/pcm"} {
		out, err := simulation.ToLiveInput(ramp(600), mime)
		if err != nil || len(samplesOf(out)) != 400 {
			t.Errorf("%q: %d samples, err %v; want 400", mime, len(samplesOf(out)), err)
		}
	}
	in := pcm(1, 2, 3, 4)
	if out, err := simulation.ToLiveInput(in, "audio/pcm;rate=16000"); err != nil || !bytes.Equal(out, in) {
		t.Errorf("16 kHz input = %v, %v; want unchanged", out, err)
	}
	if _, err := simulation.ToLiveInput(in, "audio/pcm;rate=0"); err == nil {
		t.Error("rate=0 accepted, want the resampler's error")
	}
	if simulation.LiveInputMIMEType != "audio/pcm;rate=16000" {
		t.Errorf("LiveInputMIMEType = %q", simulation.LiveInputMIMEType)
	}
}
