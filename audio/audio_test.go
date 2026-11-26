package audio

import (
	"encoding/binary"
	"testing"
)

func TestWaveform(t *testing.T) {
	pcm := make([]byte, 0, 8*2)
	for _, v := range []int16{0, 0, 100, -100, 50, -50, 0, 0} {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(v))
	}
	got := waveform(pcm, 4)
	want := []byte{0, 100, 50, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("waveform = %v, want %v", got, want)
		}
	}
	if w := waveform(nil, 64); len(w) != 64 {
		t.Fatalf("empty input gave %d bars", len(w))
	}
}
