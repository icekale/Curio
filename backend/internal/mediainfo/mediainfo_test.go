package mediainfo

import "testing"

func TestNormalizeProbeOutput(t *testing.T) {
	info := normalize(probeOutput{Streams: []probeStream{
		{
			CodecType:     "video",
			CodecName:     "hevc",
			Width:         3840,
			Height:        2160,
			ColorTransfer: "smpte2084",
			SideDataList:  []map[string]any{{"side_data_type": "DOVI configuration record"}},
		},
		{
			CodecType:     "audio",
			CodecName:     "truehd",
			Channels:      8,
			ChannelLayout: "7.1",
			Disposition:   map[string]int{"default": 1},
		},
	}})
	if info.Resolution != "2160p" || info.VideoCodec != "HEVC" || info.AudioCodec != "TrueHD" || info.AudioChannels != "7.1" || info.HDRFormat != "DV HDR10" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestVideoCodecNamesUseDisplayStandards(t *testing.T) {
	cases := map[string]string{
		"h264":       "AVC",
		"avc1":       "AVC",
		"hevc":       "HEVC",
		"hvc1":       "HEVC",
		"h266":       "VVC",
		"mpeg2video": "MPEG-2",
		"vc1":        "VC-1",
	}
	for codec, want := range cases {
		if got := videoCodec(probeStream{CodecName: codec}); got != want {
			t.Fatalf("codec %s = %s, want %s", codec, got, want)
		}
	}
}

func TestNormalizeAudioPrefersDefaultStream(t *testing.T) {
	info := normalize(probeOutput{Streams: []probeStream{
		{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080},
		{CodecType: "audio", CodecName: "dts", Profile: "DTS-HD MA", Channels: 8},
		{CodecType: "audio", CodecName: "eac3", Channels: 6, Disposition: map[string]int{"default": 1}},
	}})
	if info.AudioCodec != "DDP" || info.AudioChannels != "5.1" {
		t.Fatalf("unexpected default audio stream: %+v", info)
	}
}

func TestHDRUsesAllVideoStreams(t *testing.T) {
	info := normalize(probeOutput{Streams: []probeStream{
		{CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160, ColorTransfer: "smpte2084"},
		{CodecType: "video", CodecName: "hevc", SideDataList: []map[string]any{{"side_data_type": "DOVI configuration record"}}},
		{CodecType: "audio", CodecName: "ac3", Channels: 6},
	}})
	if info.HDRFormat != "DV HDR10" {
		t.Fatalf("unexpected hdr format: %+v", info)
	}
}
