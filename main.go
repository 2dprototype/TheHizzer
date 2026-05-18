package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"github.com/gonutz/wui/v2"
)

// --- Configuration Types ---

type Config struct {
	BackgroundImage string
	RTMPURL         string
	MorseCode       string
	FPS             int
	VideoBitrate    string
	AudioBitrate    string
	Duration        int
	OutputFile      string
	UseLiveMic      bool
}

type AppConfig struct {
	Processor       AudioProcessor `json:"processor"`
	MorseMessage    string         `json:"morse_message"`
	BackgroundImage string         `json:"background_image"`
	RTMPURL         string         `json:"rtmp_url"`
	OutputFile      string         `json:"output_file"`
	ExternalAudio   string         `json:"external_audio"`
	ExternalAudio2  string         `json:"external_audio_2"`
	Duration        int            `json:"duration"`
	VisualizerMode  int            `json:"visualizer_mode"`
	UseLiveMic      bool           `json:"use_live_mic"`
}

func getConfigPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "hizzer.config.json"
	}
	dir := filepath.Dir(exePath)
	name := filepath.Base(exePath)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return filepath.Join(dir, base+".config.json")
}

func loadConfig() AppConfig {
	path := getConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{
			Processor:       *audioProc,
			MorseMessage:    "Hello World HZZ HZZZ",
			BackgroundImage: "background.jpg",
			RTMPURL:         "rtmp://a.rtmp.youtube.com/live2/YOUR_STREAM_KEY",
			Duration:        0,
			VisualizerMode:  0,
		}
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{
			Processor:       *audioProc,
			MorseMessage:    "CQ CQ DE HIZZER",
			BackgroundImage: "background.jpg",
			RTMPURL:         "rtmp://a.rtmp.youtube.com/live2/YOUR_STREAM_KEY",
			Duration:        0,
			VisualizerMode:  0,
		}
	}
	return cfg
}

func saveConfig(cfg AppConfig) {
	path := getConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
}

// --- Audio Processing Parameters ---

type AudioProcessor struct {
	// HIZZER specific parameters
	BuzzerEnabled     bool    `json:"buzzer_enabled"`
	BuzzerFrequency   float64 `json:"buzzer_frequency"` // 4625 Hz typical HIZZER carrier
	BuzzerPulseRate   float64 `json:"buzzer_pulse_rate"` // Pulses per second
	BuzzerModDepth    float64 `json:"buzzer_mod_depth"` // Modulation depth

	// Filter parameters
	LowPassEnabled    bool    `json:"low_pass_enabled"`
	LowPassCutoff     float64 `json:"low_pass_cutoff"` // Hz
	HighPassEnabled   bool    `json:"high_pass_enabled"`
	HighPassCutoff    float64 `json:"high_pass_cutoff"` // Hz
	BandPassEnabled   bool    `json:"band_pass_enabled"`
	BandPassCenter    float64 `json:"band_pass_center"` // Hz
	BandPassQ         float64 `json:"band_pass_q"` // Quality factor

	// Distortion effects
	DistortionEnabled bool    `json:"distortion_enabled"`
	DistortionAmount  float64 `json:"distortion_amount"` // 0-1
	BitCrushEnabled   bool    `json:"bit_crush_enabled"`
	BitCrushDepth     int     `json:"bit_crush_depth"` // Bit reduction
	SampleRateReduceEnabled bool `json:"sample_rate_reduce_enabled"`
	SampleRateReduce  int     `json:"sample_rate_reduce"` // Sample rate reduction

	// Modulation effects
	RingModEnabled        bool    `json:"ring_mod_enabled"`
	RingModFreq           float64 `json:"ring_mod_freq"` // Hz
	AmplitudeModEnabled   bool    `json:"amplitude_mod_enabled"`
	AmplitudeModFreq      float64 `json:"amplitude_mod_freq"` // Hz
	AmplitudeModDepth     float64 `json:"amplitude_mod_depth"` // 0-1
	PhaseModEnabled       bool    `json:"phase_mod_enabled"`
	PhaseModFreq          float64 `json:"phase_mod_freq"` // Hz
	PhaseModDepth         float64 `json:"phase_mod_depth"` // radians

	// Time-based effects
	ReverbEnabled     bool    `json:"reverb_enabled"`
	ReverbAmount      float64 `json:"reverb_amount"` // 0-1
	ReverbDecay       float64 `json:"reverb_decay"` // seconds
	DelayEnabled      bool    `json:"delay_enabled"`
	DelayTime         float64 `json:"delay_time"` // seconds
	DelayFeedback     float64 `json:"delay_feedback"` // 0-1
	DelayAmount       float64 `json:"delay_amount"` // 0-1

	// Noise generation
	NoiseFloorEnabled bool    `json:"noise_floor_enabled"`
	NoiseFloor        float64 `json:"noise_floor"` // -60 to 0 dB
	PulseInterference bool    `json:"pulse_interference"` // Simulated pulse interference
	StaticCrackleEnabled bool `json:"static_crackle_enabled"`
	StaticCrackle     float64 `json:"static_crackle"` // Static amount 0-1

	// EQ bands
	EqEnabled         bool    `json:"eq_enabled"`
	EqBassGain        float64 `json:"eq_bass_gain"` // dB
	EqMidGain         float64 `json:"eq_mid_gain"` // dB
	EqTrebleGain      float64 `json:"eq_treble_gain"` // dB

	// Compression
	CompressorEnabled   bool    `json:"compressor_enabled"`
	CompressorThreshold float64 `json:"compressor_threshold"` // -60 to 0 dB
	CompressorRatio     float64 `json:"compressor_ratio"` // 1:1 to 20:1
	CompressorAttack    float64 `json:"compressor_attack"` // ms
	CompressorRelease   float64 `json:"compressor_release"` // ms

	// Special effects
	WowFlutterEnabled  bool   `json:"wow_flutter_enabled"` // Tape wow/flutter simulation
	RadioFading        bool   `json:"radio_fading"` // Simulate HF radio fading
	FilterSweepEnabled bool   `json:"filter_sweep_enabled"` // Sweeping filter effect

	// New Special Filters
	RadioNoiseEnabled     bool `json:"radio_noise_enabled"`
	WalkieTalkieEnabled   bool `json:"walkie_talkie_enabled"`
	DistortedAudioEnabled bool `json:"distorted_audio_enabled"`

	// Beep and Mix settings
	BeepVolume           float64 `json:"beep_volume"`
	BeepFrequency        float64 `json:"beep_frequency"`
	BeepDotDuration      float64 `json:"beep_dot_duration"`
	ExternalAudioVolume  float64 `json:"external_audio_volume"`
	ExternalAudio2Volume float64 `json:"external_audio_2_volume"`
}

// --- Global State Variables ---

var (
	morseCodeMap = map[rune]string{
		'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".",
		'F': "..-.", 'G': "--.", 'H': "....", 'I': "..", 'J': ".---",
		'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.", 'O': "---",
		'P': ".--.", 'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-",
		'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-", 'Y': "-.--",
		'Z': "--..", '0': "-----", '1': ".----", '2': "..---", '3': "...--",
		'4': "....-", '5': ".....", '6': "-....", '7': "--...", '8': "---..",
		'9': "----.",
	}

	// Audio engine parameters
	sampleRate     = 44100
	beepFormat     = beep.Format{SampleRate: beep.SampleRate(sampleRate), NumChannels: 1, Precision: 2}
	audioSamples   []float64
	processedAudio []float64
	waveformData   []float64
	spectrumData   []float64
	spectrumPeaks  []float64
	waterfallData  [][]float64
	visualizerMode = 0 // 0: Waveform, 1: Spectrum Analyzer, 2: Waterfall Spectrogram
	// Add after existing visualizerMode declaration
	waterfallRows     = 120
	waterfallBins     = 80
	waterfallScroll   = 0
	waveformDirty  = true
	spectrumDirty  = true
	waterfallDirty = true
	zoomLevel      = 1.0
	zoomOffset     = 0.0
	isStreaming    = false
	streamCancel   context.CancelFunc
	globalPaintBox *wui.PaintBox

	// Live Mic capture state
	liveMicMutex  sync.RWMutex
	liveMicCmd    *exec.Cmd
	liveMicCancel context.CancelFunc
	liveMicData   []float64
	liveMicQueue  []float64

	// Broadcast/Playback monitor state
	broadcastMutex sync.RWMutex
	broadcastData  []float64

	// Playback control
	playbackController *PlaybackController

	// Mutex for thread-safe audio access
	audioMutex sync.RWMutex
	
	// UI update channel
	uiUpdateChan = make(chan func(), 100)

	// Audio stream versioning for live updates
	audioVersion = 0

	// Audio processor instance
	audioProc = &AudioProcessor{
		BuzzerEnabled:           true,
		BuzzerFrequency:         4625.0,
		BuzzerPulseRate:         1.0,
		BuzzerModDepth:          0.8,
		LowPassEnabled:          true,
		LowPassCutoff:           3400.0,
		HighPassEnabled:         true,
		HighPassCutoff:          300.0,
		BandPassEnabled:         true,
		BandPassCenter:          2000.0,
		BandPassQ:               1.0,
		DistortionEnabled:       true,
		DistortionAmount:        0.3,
		BitCrushEnabled:         true,
		BitCrushDepth:           12,
		SampleRateReduceEnabled: true,
		SampleRateReduce:        11025,
		RingModEnabled:          true,
		RingModFreq:             50.0,
		AmplitudeModEnabled:     true,
		AmplitudeModFreq:        2.0,
		AmplitudeModDepth:       0.5,
		PhaseModEnabled:         false,
		PhaseModFreq:            1.0,
		PhaseModDepth:           0.5,
		ReverbEnabled:           true,
		ReverbAmount:            0.2,
		ReverbDecay:             1.5,
		DelayEnabled:            true,
		DelayTime:               0.25,
		DelayFeedback:           0.3,
		DelayAmount:             0.15,
		NoiseFloorEnabled:       true,
		NoiseFloor:              -40.0,
		PulseInterference:       true,
		StaticCrackleEnabled:    true,
		StaticCrackle:           0.1,
		EqEnabled:               true,
		EqBassGain:              -3.0,
		EqMidGain:               2.0,
		EqTrebleGain:            -2.0,
		CompressorEnabled:       true,
		CompressorThreshold:     -12.0,
		CompressorRatio:         4.0,
		CompressorAttack:        5.0,
		CompressorRelease:       50.0,
		WowFlutterEnabled:       true,
		RadioFading:             true,
		FilterSweepEnabled:      false,
		RadioNoiseEnabled:       false,
		WalkieTalkieEnabled:     false,
		DistortedAudioEnabled:   false,
		BeepVolume:              0.8,
		BeepFrequency:           800.0,
		BeepDotDuration:         0.1,
		ExternalAudioVolume:     0.8,
	}
)

// PlaybackController manages audio playback state
type PlaybackController struct {
	mu            sync.Mutex
	isPlaying     bool
	isPaused      bool
	streamer      *SliceStreamer
	ctrl          *beep.Ctrl
	playbackPos   int
	pausedSamples []float64
	callback      func()
}

func NewPlaybackController() *PlaybackController {
	return &PlaybackController{
		isPlaying: false,
		isPaused:  false,
	}
}

func (pc *PlaybackController) Play(samples []float64, onComplete func()) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.isPlaying {
		pc.Stop()
	}

	pc.callback = onComplete
	pc.pausedSamples = nil
	
	// Create a copy of samples to avoid race conditions
	samplesCopy := make([]float64, len(samples))
	copy(samplesCopy, samples)
	
	pc.streamer = &SliceStreamer{samples: samplesCopy, pos: 0}
	pc.ctrl = &beep.Ctrl{Streamer: pc.streamer, Paused: false}
	
	speaker.Clear()
	speaker.Play(beep.Seq(pc.ctrl, beep.Callback(func() {
		pc.mu.Lock()
		pc.isPlaying = false
		pc.isPaused = false
		callback := pc.callback
		pc.callback = nil
		pc.mu.Unlock()
		if callback != nil {
			callback()
		}
	})))
	
	pc.isPlaying = true
	pc.isPaused = false
}

func (pc *PlaybackController) Pause() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	if pc.isPlaying && !pc.isPaused && pc.ctrl != nil {
		pc.ctrl.Paused = true
		pc.isPaused = true
		
		// Save current position
		if pc.streamer != nil {
			pc.playbackPos = pc.streamer.pos
		}
	}
}

func (pc *PlaybackController) Resume() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	if pc.isPlaying && pc.isPaused && pc.ctrl != nil {
		pc.ctrl.Paused = false
		pc.isPaused = false
	}
}

func (pc *PlaybackController) Stop() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	if pc.isPlaying {
		speaker.Clear()
		pc.isPlaying = false
		pc.isPaused = false
		pc.playbackPos = 0
	}
}

func (pc *PlaybackController) IsPlaying() bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.isPlaying
}

func (pc *PlaybackController) IsPaused() bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.isPaused
}

func (pc *PlaybackController) GetPlaybackProgress() float64 {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if !pc.isPlaying || pc.streamer == nil {
		return 0.0
	}
	if len(pc.streamer.samples) == 0 {
		return 0.0
	}
	return float64(pc.streamer.pos) / float64(len(pc.streamer.samples))
}

// --- HIZZER Style Audio Processing Functions ---

func applyLowPassFilter(samples []float64, cutoffHz float64) []float64 {
	if cutoffHz >= float64(sampleRate)/2 {
		return samples
	}

	rc := 1.0 / (2 * math.Pi * cutoffHz)
	dt := 1.0 / float64(sampleRate)
	alpha := dt / (rc + dt)

	filtered := make([]float64, len(samples))
	filtered[0] = samples[0]

	for i := 1; i < len(samples); i++ {
		filtered[i] = filtered[i-1] + alpha*(samples[i]-filtered[i-1])
	}

	return filtered
}

func applyHighPassFilter(samples []float64, cutoffHz float64) []float64 {
	if cutoffHz <= 0 {
		return samples
	}

	rc := 1.0 / (2 * math.Pi * cutoffHz)
	dt := 1.0 / float64(sampleRate)
	alpha := rc / (rc + dt)

	filtered := make([]float64, len(samples))
	filtered[0] = samples[0]

	for i := 1; i < len(samples); i++ {
		filtered[i] = alpha*(filtered[i-1]+samples[i]-samples[i-1])
	}

	return filtered
}

func applyBandPassFilter(samples []float64, centerHz float64, q float64) []float64 {
	if centerHz <= 0 || q <= 0 {
		return samples
	}

	bandwidth := centerHz / q
	lowCutoff := centerHz - bandwidth/2
	highCutoff := centerHz + bandwidth/2

	if lowCutoff < 0 {
		lowCutoff = 0
	}

	filtered := applyLowPassFilter(samples, highCutoff)
	filtered = applyHighPassFilter(filtered, lowCutoff)

	return filtered
}

func applyDistortion(samples []float64, amount float64) []float64 {
	distorted := make([]float64, len(samples))

	for i, sample := range samples {
		// Soft clipping with tanh
		distorted[i] = math.Tanh(sample * (1 + amount*5))
		// Mix with original
		distorted[i] = distorted[i]*amount + sample*(1-amount)
	}

	return distorted
}

func applyBitCrushing(samples []float64, bitDepth int) []float64 {
	if bitDepth <= 0 || bitDepth >= 16 {
		return samples
	}

	levels := float64(uint(1) << uint(bitDepth))
	crushed := make([]float64, len(samples))

	for i, sample := range samples {
		// Quantize to bit depth
		quantized := math.Floor(sample*levels/2 + levels/2)
		crushed[i] = (quantized*2/levels) - 1
	}

	return crushed
}

func applySampleRateReduction(samples []float64, targetRate int) []float64 {
	if targetRate >= sampleRate {
		return samples
	}

	step := sampleRate / targetRate
	if step < 1 {
		step = 1
	}

	reduced := make([]float64, len(samples))
	lastValue := 0.0

	for i := 0; i < len(samples); i++ {
		if i%step == 0 {
			lastValue = samples[i]
		}
		reduced[i] = lastValue
	}

	return reduced
}

func applyRingModulation(samples []float64, freqHz float64) []float64 {
	modulated := make([]float64, len(samples))

	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)
		carrier := math.Sin(2 * math.Pi * freqHz * t)
		modulated[i] = samples[i] * carrier
	}

	return modulated
}

func applyAmplitudeModulation(samples []float64, freqHz float64, depth float64) []float64 {
	modulated := make([]float64, len(samples))

	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)
		modulator := 1 + depth*math.Sin(2*math.Pi*freqHz*t)
		modulated[i] = samples[i] * modulator
	}

	return modulated
}

func applyPhaseModulation(samples []float64, freqHz float64, depth float64) []float64 {
	modulated := make([]float64, len(samples))

	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)
		phaseShift := depth * math.Sin(2*math.Pi*freqHz*t)
		// Interpolate for phase shift (simplified)
		shiftSamples := int(phaseShift * float64(sampleRate) / (2 * math.Pi))
		idx := i + shiftSamples
		if idx >= 0 && idx < len(samples) {
			modulated[i] = samples[idx]
		} else {
			modulated[i] = samples[i]
		}
	}

	return modulated
}

func applyReverb(samples []float64, amount float64, decaySec float64) []float64 {
	if amount <= 0 {
		return samples
	}

	delaySamples := int(float64(sampleRate) * 0.05) // 50ms pre-delay
	decaySamples := int(float64(sampleRate) * decaySec)

	reverbed := make([]float64, len(samples))
	copy(reverbed, samples)

	// Simple convolution reverb simulation
	for i := 0; i < len(samples); i++ {
		if i+delaySamples < len(samples) {
			for j := 1; j <= 10; j++ {
				delayIndex := i + delaySamples*j
				if delayIndex < len(samples) {
					decay := math.Exp(-float64(j) / float64(decaySamples) * 10)
					reverbed[delayIndex] += samples[i] * amount * decay / float64(j)
				}
			}
		}
	}

	// Mix wet/dry
	for i := range samples {
		reverbed[i] = samples[i]*(1-amount) + reverbed[i]*amount
	}

	return reverbed
}

func applyDelay(samples []float64, delaySec float64, feedback float64, amount float64) []float64 {
	if amount <= 0 {
		return samples
	}

	delaySamples := int(float64(sampleRate) * delaySec)
	if delaySamples <= 0 {
		return samples
	}

	delayed := make([]float64, len(samples))
	copy(delayed, samples)

	delayBuffer := make([]float64, delaySamples+1)
	writePos := 0

	for i := 0; i < len(samples); i++ {
		readPos := writePos
		var delayedSample float64

		if writePos-1 >= 0 {
			delayedSample = delayBuffer[readPos]
		}

		delayBuffer[writePos] = samples[i] + delayedSample*feedback
		delayed[i] = samples[i] + delayedSample*amount

		writePos++
		if writePos > delaySamples {
			writePos = 0
		}
	}

	return delayed
}

func applyRadioNoise(samples []float64) []float64 {
	out := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)

		// 1. Power line hum: 50 Hz + 100 Hz harmonic
		hum := 0.04 * math.Sin(2*math.Pi*50*t)
		hum += 0.015 * math.Sin(2*math.Pi*100*t)

		// 2. High frequency RF heterodyne whistle (tuning whistle)
		whistle := 0.008 * math.Sin(2*math.Pi*3200*t)

		// 3. Atmospheric noise hiss
		hiss := (rand.Float64()*2 - 1) * 0.08

		// Mix original sample with hum, whistle, and hiss
		out[i] = samples[i] + hum + whistle + hiss
	}
	return out
}

func applyWalkieTalkie(samples []float64) []float64 {
	// First, steep band-pass by applying HPF at 600 Hz and LPF at 2200 Hz
	hp := applyHighPassFilter(samples, 600.0)
	lp := applyLowPassFilter(hp, 2200.0)

	out := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		val := lp[i]

		// Saturate/Distort heavily to simulate carbon mic
		if val > 0.15 {
			val = 0.15 + (val-0.15)*0.1
		} else if val < -0.15 {
			val = -0.15 + (val+0.15)*0.1
		}

		out[i] = val * 3.0
	}
	return out
}

func applyDistortedAudio(samples []float64) []float64 {
	out := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		val := samples[i]

		// Wavefolding + Hard distortion: high gain tanh shaping
		val = math.Tanh(val * 10.0)

		// Add asymmetric fuzz clipping
		if val > 0.8 {
			val = 0.8
		} else if val < -0.7 {
			val = -0.7
		}

		out[i] = val * 0.9
	}
	return out
}

func addNoiseFloor(samples []float64, noiseDb float64) []float64 {
	if noiseDb >= 0 {
		return samples
	}

	noiseAmplitude := math.Pow(10, noiseDb/20)
	noisy := make([]float64, len(samples))

	for i, sample := range samples {
		noise := (rand.Float64()*2 - 1) * noiseAmplitude
		noisy[i] = sample + noise
	}

	return noisy
}

func addPulseInterference(samples []float64, intensity float64) []float64 {
	if intensity <= 0 {
		return samples
	}

	interfered := make([]float64, len(samples))
	copy(interfered, samples)

	pulseInterval := int(float64(sampleRate) * 0.5) // Every 0.5 seconds
	pulseDuration := int(float64(sampleRate) * 0.01) // 10ms pulses

	for i := 0; i < len(samples); i++ {
		if i%pulseInterval < pulseDuration {
			pulseAmp := intensity * (1 - float64(i%pulseInterval)/float64(pulseDuration))
			interfered[i] += pulseAmp * math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))
		}
	}

	return interfered
}

func applyStaticCrackle(samples []float64, amount float64) []float64 {
	if amount <= 0 {
		return samples
	}

	crackly := make([]float64, len(samples))

	for i, sample := range samples {
		if rand.Float64() < amount*0.01 {
			// Pop/crackle
			pop := (rand.Float64()*2 - 1) * 0.5 * amount
			crackly[i] = sample + pop
		} else {
			crackly[i] = sample
		}
	}

	return crackly
}

func applyEq(samples []float64, bassGain, midGain, trebleGain float64) []float64 {
	if bassGain == 0 && midGain == 0 && trebleGain == 0 {
		return samples
	}

	// Convert dB to linear gain
	bassGainLinear := math.Pow(10, bassGain/20)
	midGainLinear := math.Pow(10, midGain/20)
	trebleGainLinear := math.Pow(10, trebleGain/20)

	// Mathematical crossover splits:
	// Low-pass at 250 Hz for Bass
	// High-pass at 4000 Hz for Treble
	// Mid is everything in-between
	bassPart := applyLowPassFilter(samples, 250.0)
	treblePart := applyHighPassFilter(samples, 4000.0)

	eqd := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		midPart := samples[i] - bassPart[i] - treblePart[i]
		eqd[i] = bassPart[i]*bassGainLinear + midPart*midGainLinear + treblePart[i]*trebleGainLinear
	}

	return eqd
}

func applyCompressor(samples []float64, thresholdDb float64, ratio float64, attackMs float64, releaseMs float64) []float64 {
	if thresholdDb >= 0 {
		return samples
	}

	thresholdLinear := math.Pow(10, thresholdDb/20)
	attackSamples := int(float64(sampleRate) * attackMs / 1000)
	releaseSamples := int(float64(sampleRate) * releaseMs / 1000)
	if attackSamples < 1 {
		attackSamples = 1
	}
	if releaseSamples < 1 {
		releaseSamples = 1
	}

	compressed := make([]float64, len(samples))
	envelope := 1.0

	for i, sample := range samples {
		absSample := math.Abs(sample)

		// Calculate gain reduction
		var gainReduction float64
		if absSample > thresholdLinear {
			excess := absSample / thresholdLinear
			compressedLevel := thresholdLinear * math.Pow(excess, 1/ratio)
			gainReduction = compressedLevel / absSample
		} else {
			gainReduction = 1.0
		}

		// Apply envelope follower
		if gainReduction < envelope {
			// Attack
			envelope = envelope - (envelope-gainReduction)/float64(attackSamples)
		} else {
			// Release
			envelope = envelope + (gainReduction-envelope)/float64(releaseSamples)
		}

		compressed[i] = sample * envelope
	}

	return compressed
}

func applyWowFlutter(samples []float64) []float64 {
	// Simulate tape wow (slow pitch variation) and flutter (fast variation)
	modulated := make([]float64, len(samples))

	wowFreq := 2.0   // Hz
	flutterFreq := 8.0 // Hz
	wowDepth := 0.005  // 0.5% pitch variation
	flutterDepth := 0.001 // 0.1% pitch variation

	readPos := 0.0
	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)

		wowMod := wowDepth * math.Sin(2*math.Pi*wowFreq*t)
		flutterMod := flutterDepth * math.Sin(2*math.Pi*flutterFreq*t)
		pitchShift := wowMod + flutterMod

		readPos += 1.0 + pitchShift

		idx1 := int(math.Floor(readPos))
		idx2 := idx1 + 1

		if idx2 < len(samples) {
			frac := readPos - float64(idx1)
			modulated[i] = samples[idx1]*(1-frac) + samples[idx2]*frac
		} else if idx1 < len(samples) {
			modulated[i] = samples[idx1]
		} else {
			wrapIdx1 := idx1 % len(samples)
			wrapIdx2 := idx2 % len(samples)
			frac := readPos - float64(idx1)
			modulated[i] = samples[wrapIdx1]*(1-frac) + samples[wrapIdx2]*frac
		}
	}

	return modulated
}

func applyRadioFading(samples []float64) []float64 {
	// Simulate HF radio fading and flutter
	faded := make([]float64, len(samples))

	fadeRate1 := 0.5  // Hz
	fadeRate2 := 0.15 // Hz
	fadeRate3 := 1.2  // Hz

	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)

		// Multi-path fading simulation
		fade1 := 0.5 + 0.5*math.Sin(2*math.Pi*fadeRate1*t)
		fade2 := 0.3 + 0.3*math.Sin(2*math.Pi*fadeRate2*t)
		fade3 := 0.2 * math.Sin(2*math.Pi*fadeRate3*t)

		fadeEnvelope := fade1 + fade2 + fade3
		if fadeEnvelope > 1.0 {
			fadeEnvelope = 1.0
		}
		if fadeEnvelope < 0.1 {
			fadeEnvelope = 0.1
		}

		faded[i] = samples[i] * fadeEnvelope
	}

	return faded
}

func applyFilterSweep(samples []float64, startFreq float64, endFreq float64, cycleSec float64) []float64 {
	swept := make([]float64, len(samples))

	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)

		// Linear sweep between frequencies
		progress := math.Mod(t, cycleSec) / cycleSec
		if progress < 0.5 {
			progress = progress * 2 // Sweep up
		} else {
			progress = 1 - (progress-0.5)*2 // Sweep down
		}

		freq := startFreq + (endFreq-startFreq)*progress

		// Simple VCF simulation (one-pole)
		rc := 1.0 / (2 * math.Pi * freq)
		dt := 1.0 / float64(sampleRate)
		alpha := dt / (rc + dt)

		if i == 0 {
			swept[i] = samples[i]
		} else {
			swept[i] = swept[i-1] + alpha*(samples[i]-swept[i-1])
		}
	}

	return swept
}

func addBuzzerCarrier(samples []float64, freqHz float64, pulseRate float64, modDepth float64) []float64 {
	buzzer := make([]float64, len(samples))

	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)

		// Pulse modulation
		pulse := 0.5 + 0.5*math.Sin(2*math.Pi*pulseRate*t)
		carrier := math.Sin(2*math.Pi*freqHz*t)
		buzzer[i] = carrier * pulse * modDepth

		// Mix with original
		buzzer[i] = samples[i] + buzzer[i]
	}

	return buzzer
}

// Main processing function that applies all effects in chain
func processAudioEffects(samples []float64, proc *AudioProcessor) []float64 {
	if len(samples) == 0 {
		return samples
	}

	processed := make([]float64, len(samples))
	copy(processed, samples)

	// 0. NEW SPECIAL FILTERS (Radio Noise, Walkie Talkie, Distorted Audio)
	if proc.RadioNoiseEnabled {
		processed = applyRadioNoise(processed)
	}

	if proc.WalkieTalkieEnabled {
		processed = applyWalkieTalkie(processed)
	}

	if proc.DistortedAudioEnabled {
		processed = applyDistortedAudio(processed)
	}

	// 1. MIXING SIGNAL SOURCES & NOISES FIRST
	if proc.BuzzerEnabled {
		processed = addBuzzerCarrier(processed, proc.BuzzerFrequency, proc.BuzzerPulseRate, proc.BuzzerModDepth)
	}

	// - Noise Floor (hzzz hzzz hiss sound)
	if proc.NoiseFloorEnabled && proc.NoiseFloor < 0 {
		processed = addNoiseFloor(processed, proc.NoiseFloor)
	}

	// - Pulse Interference
	if proc.PulseInterference {
		processed = addPulseInterference(processed, 0.15)
	}

	// - Static Crackle
	if proc.StaticCrackleEnabled && proc.StaticCrackle > 0 {
		processed = applyStaticCrackle(processed, proc.StaticCrackle)
	}

	// 2. APPLYING FILTERS ON THE MIXED AUDIO
	// - Low-Pass Filter
	if proc.LowPassEnabled && proc.LowPassCutoff < float64(sampleRate)/2 {
		processed = applyLowPassFilter(processed, proc.LowPassCutoff)
	}

	// - High-Pass Filter
	if proc.HighPassEnabled && proc.HighPassCutoff > 0 {
		processed = applyHighPassFilter(processed, proc.HighPassCutoff)
	}

	// - Band-Pass Filter
	if proc.BandPassEnabled && proc.BandPassCenter > 0 {
		processed = applyBandPassFilter(processed, proc.BandPassCenter, proc.BandPassQ)
	}

	// - Filter Sweep
	if proc.FilterSweepEnabled {
		processed = applyFilterSweep(processed, proc.LowPassCutoff, 4000, 3.0)
	}

	// 3. ANALOG GRIT & DYNAMICS
	// - Distortion
	if proc.DistortionEnabled && proc.DistortionAmount > 0 {
		processed = applyDistortion(processed, proc.DistortionAmount)
	}

	// - Bit-Crushing & Sample-Rate Reduction
	if proc.BitCrushEnabled && proc.BitCrushDepth < 16 {
		processed = applyBitCrushing(processed, proc.BitCrushDepth)
	}

	if proc.SampleRateReduceEnabled && proc.SampleRateReduce < sampleRate {
		processed = applySampleRateReduction(processed, proc.SampleRateReduce)
	}

	// - Proper 3-Band Parametric EQ (fixed time-domain bug!)
	if proc.EqEnabled {
		processed = applyEq(processed, proc.EqBassGain, proc.EqMidGain, proc.EqTrebleGain)
	}

	// - Modulation effects
	if proc.RingModEnabled && proc.RingModFreq > 0 {
		processed = applyRingModulation(processed, proc.RingModFreq)
	}

	if proc.AmplitudeModEnabled && proc.AmplitudeModFreq > 0 && proc.AmplitudeModDepth > 0 {
		processed = applyAmplitudeModulation(processed, proc.AmplitudeModFreq, proc.AmplitudeModDepth)
	}

	if proc.PhaseModEnabled && proc.PhaseModFreq > 0 && proc.PhaseModDepth > 0 {
		processed = applyPhaseModulation(processed, proc.PhaseModFreq, proc.PhaseModDepth)
	}

	// - Space & Time Effects
	if proc.DelayEnabled && proc.DelayAmount > 0 {
		processed = applyDelay(processed, proc.DelayTime, proc.DelayFeedback, proc.DelayAmount)
	}

	if proc.ReverbEnabled && proc.ReverbAmount > 0 {
		processed = applyReverb(processed, proc.ReverbAmount, proc.ReverbDecay)
	}

	// - Wow & Flutter Tape Simulation (fixed phase-warping alien sound bug!)
	if proc.WowFlutterEnabled {
		processed = applyWowFlutter(processed)
	}

	// - HF Radio Fading
	if proc.RadioFading {
		processed = applyRadioFading(processed)
	}

	// - Dynamic Compressor
	if proc.CompressorEnabled {
		processed = applyCompressor(processed, proc.CompressorThreshold, proc.CompressorRatio,
			proc.CompressorAttack, proc.CompressorRelease)
	}

	// Final normalization to prevent clipping
	maxVal := 0.0
	for _, sample := range processed {
		if absVal := math.Abs(sample); absVal > maxVal {
			maxVal = absVal
		}
	}

	if maxVal > 0.001 {
		normalizationFactor := 0.95 / maxVal
		for i := range processed {
			processed[i] *= normalizationFactor
		}
	}

	return processed
}

// --- Morse Code Logic (Modified to use processed audio)---

func textToMorse(text string) string {
	text = strings.ToUpper(text)
	var morse []string
	for _, char := range text {
		if code, exists := morseCodeMap[char]; exists {
			morse = append(morse, code)
		} else if char == ' ' {
			morse = append(morse, " ")
		}
	}
	return strings.Join(morse, " ")
}

func generateAudioBuffers(morseCode string, externalAudioPath1 string, externalAudioPath2 string) []float64 {
	freq := audioProc.BeepFrequency
	if freq <= 0 {
		freq = 800.0
	}
	dotDuration := audioProc.BeepDotDuration
	if dotDuration <= 0 {
		dotDuration = 0.1
	}
	beepVol := audioProc.BeepVolume
	externalVol1 := audioProc.ExternalAudioVolume
	externalVol2 := audioProc.ExternalAudio2Volume

	dotSamples := int(float64(sampleRate) * dotDuration)
	dashSamples := dotSamples * 3
	silenceSamples := dotSamples

	var morseSamples []float64

	generateTone := func(numSamples int) []float64 {
		res := make([]float64, numSamples)
		fadeSamples := int(float64(sampleRate) * 0.01)
		for i := 0; i < numSamples; i++ {
			t := float64(i) / float64(sampleRate)
			val := math.Sin(2 * math.Pi * freq * t) * beepVol

			if i < fadeSamples {
				val *= float64(i) / float64(fadeSamples)
			} else if i > numSamples-fadeSamples {
				val *= float64(numSamples-i) / float64(fadeSamples)
			}
			res[i] = val
		}
		return res
	}

	generateSilence := func(numSamples int) []float64 {
		return make([]float64, numSamples)
	}

	for _, symbol := range morseCode {
		switch symbol {
		case '.':
			morseSamples = append(morseSamples, generateTone(dotSamples)...)
			morseSamples = append(morseSamples, generateSilence(silenceSamples)...)
		case '-':
			morseSamples = append(morseSamples, generateTone(dashSamples)...)
			morseSamples = append(morseSamples, generateSilence(silenceSamples)...)
		case ' ':
			morseSamples = append(morseSamples, generateSilence(dotSamples*3)...)
		}
	}

	if len(morseSamples) > 0 {
		morseSamples = append(morseSamples, generateSilence(dotSamples*7)...)
	}

	// Load first external audio if provided
	var externalSamples1 []float64
	if externalAudioPath1 != "" {
		if ext, err := loadExternalAudio(externalAudioPath1); err == nil {
			externalSamples1 = ext
			for i := range externalSamples1 {
				externalSamples1[i] *= externalVol1
			}
		}
	}

	// Load second external audio if provided
	var externalSamples2 []float64
	if externalAudioPath2 != "" {
		if ext, err := loadExternalAudio(externalAudioPath2); err == nil {
			externalSamples2 = ext
			for i := range externalSamples2 {
				externalSamples2[i] *= externalVol2
			}
		}
	}

	// Mix all three signals
	var mixed []float64
	
	// Find the maximum length among all audio sources
	maxLen := len(morseSamples)
	if len(externalSamples1) > maxLen {
		maxLen = len(externalSamples1)
	}
	if len(externalSamples2) > maxLen {
		maxLen = len(externalSamples2)
	}
	
	if maxLen > 0 {
		mixed = make([]float64, maxLen)
		
		// Mix Morse code
		for i := 0; i < maxLen && i < len(morseSamples); i++ {
			mixed[i] += morseSamples[i]
		}
		
		// Mix first external audio
		if len(externalSamples1) > 0 {
			for i := 0; i < maxLen; i++ {
				mixed[i] += externalSamples1[i%len(externalSamples1)]
			}
		}
		
		// Mix second external audio
		if len(externalSamples2) > 0 {
			for i := 0; i < maxLen; i++ {
				mixed[i] += externalSamples2[i%len(externalSamples2)]
			}
		}
	}

	// Apply audio processing chain
	processed := processAudioEffects(mixed, audioProc)

	return processed
}

// --- Audio Visualizer Layout Math ---

func computeWaveform(samples []float64, canvasWidth int) []float64 {
	if len(samples) == 0 {
		return make([]float64, canvasWidth)
	}

	sampleStep := int(float64(len(samples)) / (float64(canvasWidth) * zoomLevel))
	if sampleStep == 0 {
		sampleStep = 1
	}

	waveform := make([]float64, canvasWidth)
	sampleCount := len(samples)

	for i := 0; i < canvasWidth; i++ {
		start := int(float64(i)*float64(sampleStep) - zoomOffset*float64(sampleStep))
		end := start + sampleStep

		if start < 0 {
			start = 0
		}
		if end > sampleCount {
			end = sampleCount
		}
		if start >= sampleCount {
			break
		}

		sum := 0.0
		for j := start; j < end; j++ {
			sum += samples[j]
		}
		waveform[i] = sum / float64(end-start)
	}

	maxAmplitude := 0.0
	for _, value := range waveform {
		if absValue := math.Abs(value); absValue > maxAmplitude {
			maxAmplitude = absValue
		}
	}
	if maxAmplitude == 0 {
		maxAmplitude = 1
	}
	for i := range waveform {
		waveform[i] /= maxAmplitude
	}

	return waveform
}

func computeSpectrum(samples []float64, numBins int) []float64 {
	bins := make([]float64, numBins)
	if len(samples) == 0 {
		return bins
	}

	windowSize := 1024
	step := 512
	if len(samples) < windowSize {
		windowSize = len(samples)
		step = windowSize
	}

	// Pre-calculate the Hann window to save thousands of math.Cos calls
	window := make([]float64, windowSize)
	for n := 0; n < windowSize; n++ {
		window[n] = 0.5 * (1.0 - math.Cos(2*math.Pi*float64(n)/float64(windowSize-1)))
	}

	count := 0
	for start := 0; start+windowSize <= len(samples); start += step {
		segment := samples[start : start+windowSize]
		for b := 0; b < numBins; b++ {
			minFreq := 40.0
			maxFreq := 12000.0
			freq := minFreq * math.Pow(maxFreq/minFreq, float64(b)/float64(numBins))
			
			realSum := 0.0
			imagSum := 0.0
			phaseStep := 2.0 * math.Pi * freq / float64(sampleRate)
			phase := 0.0
			
			// Optimized inner loop
			for n := 0; n < windowSize; n++ {
				realSum += segment[n] * window[n] * math.Cos(phase)
				imagSum += segment[n] * window[n] * math.Sin(phase)
				phase += phaseStep
			}
			mag := math.Sqrt(realSum*realSum + imagSum*imagSum) / float64(windowSize)
			bins[b] += mag
		}
		count++
		if count > 200 { 
			break
		}
	}

	if count > 0 {
		for b := 0; b < numBins; b++ {
			bins[b] /= float64(count)
			bins[b] = math.Log10(1.0 + bins[b]*60.0)
			if bins[b] > 1.0 {
				bins[b] = 1.0
			}
		}
	}

	return bins
}

func computeWaterfall(samples []float64, numRows int, numBins int) [][]float64 {
	waterfall := make([][]float64, numRows)
	for i := range waterfall {
		waterfall[i] = make([]float64, numBins)
	}
	if len(samples) == 0 {
		return waterfall
	}

	samplesPerRow := sampleRate / 20 // 50ms per row
	totalRows := len(samples) / samplesPerRow
	if totalRows < numRows {
		totalRows = numRows
	}

	// Pre-calculate Hann window up to max expected size
	maxWinSize := 1024
	window := make([]float64, maxWinSize)
	for n := 0; n < maxWinSize; n++ {
		window[n] = 0.5 * (1.0 - math.Cos(2*math.Pi*float64(n)/float64(maxWinSize-1)))
	}

	for r := 0; r < numRows && r < totalRows; r++ {
		sourceRow := totalRows - numRows + r
		if sourceRow < 0 {
			continue
		}
		
		start := sourceRow * samplesPerRow
		end := start + samplesPerRow
		if end > len(samples) {
			end = len(samples)
		}
		if start >= len(samples) {
			break
		}

		segment := samples[start:end]
		winSize := len(segment)
		if winSize == 0 {
			continue
		}
		if winSize > maxWinSize {
			winSize = maxWinSize
		}

		for b := 0; b < numBins; b++ {
			minFreq := 20.0
			maxFreq := 12000.0
			freq := minFreq * math.Pow(maxFreq/minFreq, float64(b)/float64(numBins))

			realSum := 0.0
			imagSum := 0.0
			phaseStep := 2.0 * math.Pi * freq / float64(sampleRate)
			phase := 0.0
			
			// Optimized inner loop
			for n := 0; n < winSize; n++ {
				realSum += segment[n] * window[n] * math.Cos(phase)
				imagSum += segment[n] * window[n] * math.Sin(phase)
				phase += phaseStep
			}
			
			mag := math.Sqrt(realSum*realSum+imagSum*imagSum) / float64(winSize)
			val := math.Log10(1.0 + mag*80.0)
			if val > 1.0 { val = 1.0 }
			if val < 0 { val = 0 }
			waterfall[r][b] = val
		}
	}
	
	return waterfall
}

func getWaterfallColor(v float64) wui.Color {
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}

	var r, g, b uint8
	
	// Enhanced color map for better visibility
	// Black -> Blue -> Cyan -> Green -> Yellow -> Red -> White
	switch {
	case v < 0.1:
		// Very dark blue to black
		pct := v / 0.1
		r = uint8(0)
		g = uint8(0)
		b = uint8(20 + pct*35)
	case v < 0.25:
		// Blue to cyan
		pct := (v - 0.1) / 0.15
		r = uint8(0)
		g = uint8(pct * 150)
		b = uint8(55 + pct*200)
	case v < 0.5:
		// Cyan to green
		pct := (v - 0.25) / 0.25
		r = uint8(pct * 80)
		g = uint8(150 + pct*105)
		b = uint8(255 - pct*255)
	case v < 0.75:
		// Green to yellow
		pct := (v - 0.5) / 0.25
		r = uint8(80 + pct*175)
		g = uint8(255)
		b = uint8(0)
	default:
		// Yellow to red to white
		pct := (v - 0.75) / 0.25
		r = uint8(255)
		g = uint8(255 - pct*255)
		b = uint8(pct * 200)
	}
	
	return wui.RGB(r, g, b)
}

func isVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv", ".wmv", ".m4v", ".mpg", ".mpeg", ".3gp":
		return true
	}
	return false
}

func getDefaultDshowAudioDevice() string {
	cmd := exec.Command("ffmpeg", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	out, _ := cmd.CombinedOutput()
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "(audio)") {
			firstQuote := strings.Index(line, "\"")
			if firstQuote != -1 {
				lastQuote := strings.Index(line[firstQuote+1:], "\"")
				if lastQuote != -1 {
					return line[firstQuote+1 : firstQuote+1+lastQuote]
				}
			}
		}
	}
	return ""
}

// --- Media Pipeline Control Engine ---

func hasAudioStream(filePath string) bool {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=codec_name", "-of", "default=noprint_wrappers=1:nokey=1", filePath)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

func executeStreamPipeline(ctx context.Context, cfg Config) error {
	var args []string
	
	isVid := isVideoFile(cfg.BackgroundImage)
	if isVid {
		args = append(args, "-re", "-stream_loop", "-1", "-i", cfg.BackgroundImage)
	} else {
		args = append(args, "-re", "-loop", "1", "-i", cfg.BackgroundImage)
	}

	// Unify: Always use pipe:0 for mixed broadcast + mic audio
	args = append(args, "-f", "s16le", "-ar", "44100", "-ac", "1", "-i", "pipe:0")

	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast",
		"-b:v", cfg.VideoBitrate, "-maxrate", cfg.VideoBitrate, "-bufsize", "6000k",
		"-pix_fmt", "yuv420p", "-g", "60", "-r", fmt.Sprintf("%d", cfg.FPS),
		"-c:a", "aac", "-b:a", cfg.AudioBitrate, "-ar", "44100",
		"-vf", fmt.Sprintf("fps=%d,scale=1920:1080,format=yuv420p,drawtext=text='%%{pts\\:localtime}':x=10:y=10:fontsize=24:fontcolor=white", cfg.FPS),
	)

	// Unified mapping for both video and image backgrounds
	hasAudio := false
	if isVid {
		hasAudio = hasAudioStream(cfg.BackgroundImage)
	}

	if isVid && hasAudio {
		// Use filter complex to mix video audio and pipe audio
		args = append(args,
			"-filter_complex", "[0:a]aresample=44100[a0];[1:a]aresample=44100[a1];[a0][a1]amix=inputs=2:dropout_transition=0,volume=2[a]",
			"-map", "0:v", "-map", "[a]",
		)
	} else {
		args = append(args,
			"-map", "0:v", "-map", "1:a",
			"-af", "aresample=44100",
		)
	}

	if cfg.Duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", cfg.Duration))
	}

	if cfg.OutputFile != "" {
		args = append(args, cfg.OutputFile)
	} else {
		args = append(args, "-f", "flv", cfg.RTMPURL)
	}
	args = append(args, "-y")

	// Ensure background live mic capture is active if required
	if cfg.UseLiveMic {
		startLiveMicCapture()
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	
	// Open log file to record any ffmpeg standard error
	errFile, _ := os.Create("ffmpeg_stream_error.log")
	cmd.Stderr = errFile
	defer errFile.Close()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		defer stdin.Close()
		
		ticker := time.NewTicker(23219 * time.Microsecond) // ~23.22 ms for 1024 samples at 44100Hz
		defer ticker.Stop()

		pos := 0
		chunkSize := 1024
		buf := make([]byte, chunkSize*2)
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				audioMutex.RLock()
				activeSamples := audioSamples
				audioMutex.RUnlock()

				// Read mic samples if the live mic is actively captured (dynamically toggled live)
				micSamples := make([]float64, chunkSize)
				liveMicMutex.Lock()
				micActive := liveMicCancel != nil
				if micActive {
					n := len(liveMicQueue)
					if n > 0 {
						if n >= chunkSize {
							copy(micSamples, liveMicQueue[:chunkSize])
							liveMicQueue = liveMicQueue[chunkSize:]
						} else {
							copy(micSamples, liveMicQueue)
							liveMicQueue = nil
						}
					}
				}
				liveMicMutex.Unlock()
				
				// Broadcast samples buffer for the visualizer
				bcastSamples := make([]float64, chunkSize)
				
				for i := 0; i < chunkSize; i++ {
					var morseVal float64
					if len(activeSamples) > 0 {
						sampleIdx := (pos + i) % len(activeSamples)
						morseVal = activeSamples[sampleIdx]
					}
					
					mixedSample := morseVal + micSamples[i]
					if mixedSample > 1.0 {
						mixedSample = 1.0
					} else if mixedSample < -1.0 {
						mixedSample = -1.0
					}
					
					bcastSamples[i] = mixedSample
					
					val := int16(mixedSample * 32767.0)
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(val))
				}
				
				// Update broadcast monitor buffer for secPaintBox
				broadcastMutex.Lock()
				broadcastData = append(broadcastData, bcastSamples...)
				if len(broadcastData) > 4096 {
					broadcastData = broadcastData[len(broadcastData)-4096:]
				}
				broadcastMutex.Unlock()
				
				if len(activeSamples) > 0 {
					pos = (pos + chunkSize) % len(activeSamples)
				}
				
				_, err := stdin.Write(buf)
				if err != nil {
					return
				}
			}
		}
	}()

	return cmd.Wait()
}

func createDefaultBackground(filename string) error {
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i", "color=c=black:s=1920x1080:d=1",
		"-frames:v", "1", filename, "-y")
	return cmd.Run()
}

func writeWavFile(filename string, samples []float64, sampleRate int) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	numSamples := len(samples)
	numChannels := 1
	bitsPerSample := 16
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	subchunk2Size := numSamples * numChannels * bitsPerSample / 8
	chunkSize := 36 + subchunk2Size

	file.WriteString("RIFF")
	binary.Write(file, binary.LittleEndian, uint32(chunkSize))
	file.WriteString("WAVE")
	file.WriteString("fmt ")
	binary.Write(file, binary.LittleEndian, uint32(16))
	binary.Write(file, binary.LittleEndian, uint16(1))
	binary.Write(file, binary.LittleEndian, uint16(numChannels))
	binary.Write(file, binary.LittleEndian, uint32(sampleRate))
	binary.Write(file, binary.LittleEndian, uint32(byteRate))
	binary.Write(file, binary.LittleEndian, uint16(blockAlign))
	binary.Write(file, binary.LittleEndian, uint16(bitsPerSample))
	file.WriteString("data")
	binary.Write(file, binary.LittleEndian, uint32(subchunk2Size))

	for _, sample := range samples {
		intSample := int16(sample * 32767)
		binary.Write(file, binary.LittleEndian, intSample)
	}

	return nil
}

func loadExternalAudio(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	streamer, format, err := wav.Decode(f)
	if err != nil {
		return nil, err
	}
	defer streamer.Close()

	var samples []float64
	buffer := make([][2]float64, 512)
	for {
		n, ok := streamer.Stream(buffer)
		if n == 0 || !ok {
			break
		}
		for i := 0; i < n; i++ {
			monoVal := (buffer[i][0] + buffer[i][1]) / 2.0
			samples = append(samples, monoVal)
		}
	}

	if int(format.SampleRate) != sampleRate {
		ratio := float64(format.SampleRate) / float64(sampleRate)
		newLen := int(float64(len(samples)) / ratio)
		if newLen > 0 {
			resampled := make([]float64, newLen)
			for i := 0; i < newLen; i++ {
				srcIdx := float64(i) * ratio
				idx1 := int(math.Floor(srcIdx))
				idx2 := idx1 + 1
				if idx2 < len(samples) {
					frac := srcIdx - float64(idx1)
					resampled[i] = samples[idx1]*(1-frac) + samples[idx2]*frac
				} else if idx1 < len(samples) {
					resampled[i] = samples[idx1]
				}
			}
			samples = resampled
		}
	}

	return samples, nil
}

type SliceStreamer struct {
	samples []float64
	pos     int
}

func (s *SliceStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if s.pos >= len(s.samples) {
		return 0, false
	}
	var playSamples []float64
	for i := range samples {
		if s.pos >= len(s.samples) {
			if len(playSamples) > 0 {
				broadcastMutex.Lock()
				broadcastData = append(broadcastData, playSamples...)
				if len(broadcastData) > 4096 {
					broadcastData = broadcastData[len(broadcastData)-4096:]
				}
				broadcastMutex.Unlock()
			}
			return i, true
		}
		val := s.samples[s.pos]
		samples[i][0] = val
		samples[i][1] = val
		playSamples = append(playSamples, val)
		s.pos++
	}
	if len(playSamples) > 0 {
		broadcastMutex.Lock()
		broadcastData = append(broadcastData, playSamples...)
		if len(broadcastData) > 4096 {
			broadcastData = broadcastData[len(broadcastData)-4096:]
		}
		broadcastMutex.Unlock()
	}
	return len(samples), true
}

func (s *SliceStreamer) Err() error { return nil }

func showAudioSettingsDialog(parent *wui.Window) {
	settingsWin := wui.NewWindow()
	settingsWin.SetInnerPosition(parent.X()+20, parent.Y()+20)
	settingsWin.SetInnerWidth(950)
	settingsWin.SetInnerHeight(490)
	settingsWin.SetResizable(false)
	settingsWin.SetHasMaxButton(false)
	settingsWin.SetTitle("Audio Processing Settings")

	font, _ := wui.NewFont(wui.FontDesc{Name: "Segoe UI", Height: -12})
	settingsWin.SetFont(font)

	// Header Panel (Y: 10 to 45)
	lblPresets := wui.NewLabel()
	lblPresets.SetBounds(20, 15, 60, 20)
	lblPresets.SetText("Presets:")
	settingsWin.Add(lblPresets)

	btnHIZZER := wui.NewButton()
	btnHIZZER.SetBounds(90, 10, 100, 28)
	btnHIZZER.SetText("HIZZER")
	settingsWin.Add(btnHIZZER)

	btnRadio := wui.NewButton()
	btnRadio.SetBounds(200, 10, 100, 28)
	btnRadio.SetText("AM Radio")
	settingsWin.Add(btnRadio)

	btnLoFi := wui.NewButton()
	btnLoFi.SetBounds(310, 10, 100, 28)
	btnLoFi.SetText("Lo-Fi")
	settingsWin.Add(btnLoFi)

	btnClean := wui.NewButton()
	btnClean.SetBounds(420, 10, 100, 28)
	btnClean.SetText("Clean")
	settingsWin.Add(btnClean)

	sepHeader := wui.NewLabel()
	sepHeader.SetBounds(20, 45, 910, 2)
	sepHeader.SetText("──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────")
	settingsWin.Add(sepHeader)

	// Column 1: Signal, Filters & Modulations (X: 20 to 240)
	lblCol1Header := wui.NewLabel()
	lblCol1Header.SetBounds(20, 60, 220, 20)
	lblCol1Header.SetText("[ 🎛️ Signal & Filters ]")
	settingsWin.Add(lblCol1Header)

	chkBuzzer := wui.NewCheckBox()
	chkBuzzer.SetBounds(20, 88, 180, 22)
	chkBuzzer.SetText("Enable Buzzer")
	chkBuzzer.SetChecked(audioProc.BuzzerEnabled)
	settingsWin.Add(chkBuzzer)

	lblBuzzerFreq := wui.NewLabel()
	lblBuzzerFreq.SetBounds(20, 116, 130, 20)
	lblBuzzerFreq.SetText("Carrier Freq (Hz):")
	settingsWin.Add(lblBuzzerFreq)

	txtBuzzerFreq := wui.NewEditLine()
	txtBuzzerFreq.SetBounds(160, 114, 70, 22)
	txtBuzzerFreq.SetText(fmt.Sprintf("%.0f", audioProc.BuzzerFrequency))
	settingsWin.Add(txtBuzzerFreq)

	lblBuzzerRate := wui.NewLabel()
	lblBuzzerRate.SetBounds(20, 144, 130, 20)
	lblBuzzerRate.SetText("Pulse Rate (Hz):")
	settingsWin.Add(lblBuzzerRate)

	txtBuzzerRate := wui.NewEditLine()
	txtBuzzerRate.SetBounds(160, 142, 70, 22)
	txtBuzzerRate.SetText(fmt.Sprintf("%.1f", audioProc.BuzzerPulseRate))
	settingsWin.Add(txtBuzzerRate)

	lblBuzzerMod := wui.NewLabel()
	lblBuzzerMod.SetBounds(20, 172, 130, 20)
	lblBuzzerMod.SetText("Modulation Depth:")
	settingsWin.Add(lblBuzzerMod)

	txtBuzzerMod := wui.NewEditLine()
	txtBuzzerMod.SetBounds(160, 170, 70, 22)
	txtBuzzerMod.SetText(fmt.Sprintf("%.2f", audioProc.BuzzerModDepth))
	settingsWin.Add(txtBuzzerMod)

	lblFiltersHeader := wui.NewLabel()
	lblFiltersHeader.SetBounds(20, 200, 220, 20)
	lblFiltersHeader.SetText("─ Filters ────────────────────")
	settingsWin.Add(lblFiltersHeader)

	chkLowPass := wui.NewCheckBox()
	chkLowPass.SetBounds(20, 226, 135, 22)
	chkLowPass.SetText("Low Pass Cutoff:")
	chkLowPass.SetChecked(audioProc.LowPassEnabled)
	settingsWin.Add(chkLowPass)

	txtLowPass := wui.NewEditLine()
	txtLowPass.SetBounds(160, 226, 70, 22)
	txtLowPass.SetText(fmt.Sprintf("%.0f", audioProc.LowPassCutoff))
	settingsWin.Add(txtLowPass)

	chkHighPass := wui.NewCheckBox()
	chkHighPass.SetBounds(20, 254, 135, 22)
	chkHighPass.SetText("High Pass Cutoff:")
	chkHighPass.SetChecked(audioProc.HighPassEnabled)
	settingsWin.Add(chkHighPass)

	txtHighPass := wui.NewEditLine()
	txtHighPass.SetBounds(160, 254, 70, 22)
	txtHighPass.SetText(fmt.Sprintf("%.0f", audioProc.HighPassCutoff))
	settingsWin.Add(txtHighPass)

	chkFilterSweep := wui.NewCheckBox()
	chkFilterSweep.SetBounds(20, 282, 180, 22)
	chkFilterSweep.SetText("Filter Sweep (VCF)")
	chkFilterSweep.SetChecked(audioProc.FilterSweepEnabled)
	settingsWin.Add(chkFilterSweep)

	lblModHeader := wui.NewLabel()
	lblModHeader.SetBounds(20, 312, 220, 20)
	lblModHeader.SetText("─ Modulation ─────────────────")
	settingsWin.Add(lblModHeader)

	chkRingMod := wui.NewCheckBox()
	chkRingMod.SetBounds(20, 338, 135, 22)
	chkRingMod.SetText("Ring Mod (Hz):")
	chkRingMod.SetChecked(audioProc.RingModEnabled)
	settingsWin.Add(chkRingMod)

	txtRingMod := wui.NewEditLine()
	txtRingMod.SetBounds(160, 338, 70, 22)
	txtRingMod.SetText(fmt.Sprintf("%.0f", audioProc.RingModFreq))
	settingsWin.Add(txtRingMod)

	chkAmpMod := wui.NewCheckBox()
	chkAmpMod.SetBounds(20, 366, 135, 22)
	chkAmpMod.SetText("Amp Mod (Hz):")
	chkAmpMod.SetChecked(audioProc.AmplitudeModEnabled)
	settingsWin.Add(chkAmpMod)

	txtAmpMod := wui.NewEditLine()
	txtAmpMod.SetBounds(160, 366, 70, 22)
	txtAmpMod.SetText(fmt.Sprintf("%.1f", audioProc.AmplitudeModFreq))
	settingsWin.Add(txtAmpMod)

	chkPhaseMod := wui.NewCheckBox()
	chkPhaseMod.SetBounds(20, 394, 135, 22)
	chkPhaseMod.SetText("Phase Mod (Hz):")
	chkPhaseMod.SetChecked(audioProc.PhaseModEnabled)
	settingsWin.Add(chkPhaseMod)

	txtPhaseMod := wui.NewEditLine()
	txtPhaseMod.SetBounds(160, 394, 70, 22)
	txtPhaseMod.SetText(fmt.Sprintf("%.1f", audioProc.PhaseModFreq))
	settingsWin.Add(txtPhaseMod)


	// Column 2: Analog Grit & EQ (X: 260 to 480)
	lblCol2Header := wui.NewLabel()
	lblCol2Header.SetBounds(260, 60, 220, 20)
	lblCol2Header.SetText("[ 🎚️ Analog Grit & EQ ]")
	settingsWin.Add(lblCol2Header)

	chkDist := wui.NewCheckBox()
	chkDist.SetBounds(260, 88, 135, 22)
	chkDist.SetText("Distortion:")
	chkDist.SetChecked(audioProc.DistortionEnabled)
	settingsWin.Add(chkDist)

	txtDist := wui.NewEditLine()
	txtDist.SetBounds(400, 88, 70, 22)
	txtDist.SetText(fmt.Sprintf("%.2f", audioProc.DistortionAmount))
	settingsWin.Add(txtDist)

	chkBitCrush := wui.NewCheckBox()
	chkBitCrush.SetBounds(260, 118, 135, 22)
	chkBitCrush.SetText("Bit Crush:")
	chkBitCrush.SetChecked(audioProc.BitCrushEnabled)
	settingsWin.Add(chkBitCrush)

	txtBitCrush := wui.NewEditLine()
	txtBitCrush.SetBounds(400, 118, 70, 22)
	txtBitCrush.SetText(fmt.Sprintf("%d", audioProc.BitCrushDepth))
	settingsWin.Add(txtBitCrush)

	chkSampleRate := wui.NewCheckBox()
	chkSampleRate.SetBounds(260, 148, 135, 22)
	chkSampleRate.SetText("Sample Rate:")
	chkSampleRate.SetChecked(audioProc.SampleRateReduceEnabled)
	settingsWin.Add(chkSampleRate)

	txtSampleRate := wui.NewEditLine()
	txtSampleRate.SetBounds(400, 148, 70, 22)
	txtSampleRate.SetText(fmt.Sprintf("%d", audioProc.SampleRateReduce))
	settingsWin.Add(txtSampleRate)

	chkEq := wui.NewCheckBox()
	chkEq.SetBounds(260, 188, 220, 22)
	chkEq.SetText("─ Equalizer (dB) ─────────────")
	chkEq.SetChecked(audioProc.EqEnabled)
	settingsWin.Add(chkEq)

	lblBass := wui.NewLabel()
	lblBass.SetBounds(260, 220, 100, 20)
	lblBass.SetText("Bass Gain:")
	settingsWin.Add(lblBass)

	txtBass := wui.NewEditLine()
	txtBass.SetBounds(400, 218, 70, 22)
	txtBass.SetText(fmt.Sprintf("%.1f", audioProc.EqBassGain))
	settingsWin.Add(txtBass)

	lblMid := wui.NewLabel()
	lblMid.SetBounds(260, 250, 100, 20)
	lblMid.SetText("Mid Gain:")
	settingsWin.Add(lblMid)

	txtMid := wui.NewEditLine()
	txtMid.SetBounds(400, 248, 70, 22)
	txtMid.SetText(fmt.Sprintf("%.1f", audioProc.EqMidGain))
	settingsWin.Add(txtMid)

	lblTreble := wui.NewLabel()
	lblTreble.SetBounds(260, 280, 100, 20)
	lblTreble.SetText("Treble Gain:")
	settingsWin.Add(lblTreble)

	txtTreble := wui.NewEditLine()
	txtTreble.SetBounds(400, 278, 70, 22)
	txtTreble.SetText(fmt.Sprintf("%.1f", audioProc.EqTrebleGain))
	settingsWin.Add(txtTreble)

	lblBeepHeader := wui.NewLabel()
	lblBeepHeader.SetBounds(260, 312, 220, 20)
	lblBeepHeader.SetText("─ Beep & External Mix ────────")
	settingsWin.Add(lblBeepHeader)

	lblBeepFreq := wui.NewLabel()
	lblBeepFreq.SetBounds(260, 338, 130, 20)
	lblBeepFreq.SetText("Beep Freq (Hz):")
	settingsWin.Add(lblBeepFreq)

	txtBeepFreq := wui.NewEditLine()
	txtBeepFreq.SetBounds(400, 338, 70, 22)
	txtBeepFreq.SetText(fmt.Sprintf("%.0f", audioProc.BeepFrequency))
	settingsWin.Add(txtBeepFreq)

	lblBeepVol := wui.NewLabel()
	lblBeepVol.SetBounds(260, 366, 130, 20)
	lblBeepVol.SetText("Beep Volume:")
	settingsWin.Add(lblBeepVol)

	txtBeepVol := wui.NewEditLine()
	txtBeepVol.SetBounds(400, 366, 70, 22)
	txtBeepVol.SetText(fmt.Sprintf("%.2f", audioProc.BeepVolume))
	settingsWin.Add(txtBeepVol)

	lblBeepDur := wui.NewLabel()
	lblBeepDur.SetBounds(260, 394, 130, 20)
	lblBeepDur.SetText("Beep Dur (s):")
	settingsWin.Add(lblBeepDur)

	txtBeepDur := wui.NewEditLine()
	txtBeepDur.SetBounds(400, 394, 70, 22)
	txtBeepDur.SetText(fmt.Sprintf("%.2f", audioProc.BeepDotDuration))
	settingsWin.Add(txtBeepDur)

	lblExtVol := wui.NewLabel()
	lblExtVol.SetBounds(260, 422, 130, 20)
	lblExtVol.SetText("Ext Audio Vol:")
	settingsWin.Add(lblExtVol)

	txtExtVol := wui.NewEditLine()
	txtExtVol.SetBounds(400, 422, 70, 22)
	txtExtVol.SetText(fmt.Sprintf("%.2f", audioProc.ExternalAudioVolume))
	settingsWin.Add(txtExtVol)
	
	lblExtVol2 := wui.NewLabel()
	lblExtVol2.SetBounds(260, 450, 130, 20)
	lblExtVol2.SetText("Ext Audio 2 Vol:")
	settingsWin.Add(lblExtVol2)

	txtExtVol2 := wui.NewEditLine()
	txtExtVol2.SetBounds(400, 448, 70, 22)
	txtExtVol2.SetText(fmt.Sprintf("%.2f", audioProc.ExternalAudio2Volume))
	settingsWin.Add(txtExtVol2)


	// Column 3: Space & Time FX (X: 500 to 720)
	lblCol3Header := wui.NewLabel()
	lblCol3Header.SetBounds(500, 60, 220, 20)
	lblCol3Header.SetText("[ 🌌 Space & Time FX ]")
	settingsWin.Add(lblCol3Header)

	chkReverb := wui.NewCheckBox()
	chkReverb.SetBounds(500, 88, 135, 22)
	chkReverb.SetText("Reverb:")
	chkReverb.SetChecked(audioProc.ReverbEnabled)
	settingsWin.Add(chkReverb)

	txtReverb := wui.NewEditLine()
	txtReverb.SetBounds(640, 88, 70, 22)
	txtReverb.SetText(fmt.Sprintf("%.2f", audioProc.ReverbAmount))
	settingsWin.Add(txtReverb)

	lblReverbDecay := wui.NewLabel()
	lblReverbDecay.SetBounds(500, 120, 130, 20)
	lblReverbDecay.SetText("Reverb Decay (s):")
	settingsWin.Add(lblReverbDecay)

	txtReverbDecay := wui.NewEditLine()
	txtReverbDecay.SetBounds(640, 118, 70, 22)
	txtReverbDecay.SetText(fmt.Sprintf("%.1f", audioProc.ReverbDecay))
	settingsWin.Add(txtReverbDecay)

	chkDelay := wui.NewCheckBox()
	chkDelay.SetBounds(500, 148, 135, 22)
	chkDelay.SetText("Delay:")
	chkDelay.SetChecked(audioProc.DelayEnabled)
	settingsWin.Add(chkDelay)

	txtDelay := wui.NewEditLine()
	txtDelay.SetBounds(640, 148, 70, 22)
	txtDelay.SetText(fmt.Sprintf("%.2f", audioProc.DelayAmount))
	settingsWin.Add(txtDelay)

	lblDelayTime := wui.NewLabel()
	lblDelayTime.SetBounds(500, 180, 130, 20)
	lblDelayTime.SetText("Delay Time (s):")
	settingsWin.Add(lblDelayTime)

	txtDelayTime := wui.NewEditLine()
	txtDelayTime.SetBounds(640, 178, 70, 22)
	txtDelayTime.SetText(fmt.Sprintf("%.2f", audioProc.DelayTime))
	settingsWin.Add(txtDelayTime)

	lblDelayFb := wui.NewLabel()
	lblDelayFb.SetBounds(500, 210, 130, 20)
	lblDelayFb.SetText("Delay Feedback:")
	settingsWin.Add(lblDelayFb)

	txtDelayFb := wui.NewEditLine()
	txtDelayFb.SetBounds(640, 208, 70, 22)
	txtDelayFb.SetText(fmt.Sprintf("%.2f", audioProc.DelayFeedback))
	settingsWin.Add(txtDelayFb)

	chkWowFlutter := wui.NewCheckBox()
	chkWowFlutter.SetBounds(500, 240, 180, 22)
	chkWowFlutter.SetText("Tape Wow / Flutter")
	chkWowFlutter.SetChecked(audioProc.WowFlutterEnabled)
	settingsWin.Add(chkWowFlutter)

	chkRadioFading := wui.NewCheckBox()
	chkRadioFading.SetBounds(500, 270, 180, 22)
	chkRadioFading.SetText("HF Radio Fading")
	chkRadioFading.SetChecked(audioProc.RadioFading)
	settingsWin.Add(chkRadioFading)


	// Column 4: Broadcast & Interference (X: 740 to 940)
	lblCol4Header := wui.NewLabel()
	lblCol4Header.SetBounds(740, 60, 200, 20)
	lblCol4Header.SetText("[ 📻 Broadcast & Special ]")
	settingsWin.Add(lblCol4Header)

	chkPulse := wui.NewCheckBox()
	chkPulse.SetBounds(740, 88, 180, 22)
	chkPulse.SetText("Pulse Interference")
	chkPulse.SetChecked(audioProc.PulseInterference)
	settingsWin.Add(chkPulse)

	chkStaticCrackle := wui.NewCheckBox()
	chkStaticCrackle.SetBounds(740, 116, 115, 22)
	chkStaticCrackle.SetText("Static Crackle:")
	chkStaticCrackle.SetChecked(audioProc.StaticCrackleEnabled)
	settingsWin.Add(chkStaticCrackle)

	txtCrackle := wui.NewEditLine()
	txtCrackle.SetBounds(860, 116, 70, 22)
	txtCrackle.SetText(fmt.Sprintf("%.2f", audioProc.StaticCrackle))
	settingsWin.Add(txtCrackle)

	chkNoiseFloor := wui.NewCheckBox()
	chkNoiseFloor.SetBounds(740, 144, 115, 22)
	chkNoiseFloor.SetText("Noise Floor:")
	chkNoiseFloor.SetChecked(audioProc.NoiseFloorEnabled)
	settingsWin.Add(chkNoiseFloor)

	txtNoise := wui.NewEditLine()
	txtNoise.SetBounds(860, 144, 70, 22)
	txtNoise.SetText(fmt.Sprintf("%.1f", audioProc.NoiseFloor))
	settingsWin.Add(txtNoise)

	lblSpecialHeader := wui.NewLabel()
	lblSpecialHeader.SetBounds(740, 174, 200, 20)
	lblSpecialHeader.SetText("─ Special Filters ────────────")
	settingsWin.Add(lblSpecialHeader)

	chkRadioNoise := wui.NewCheckBox()
	chkRadioNoise.SetBounds(740, 200, 180, 22)
	chkRadioNoise.SetText("📻 Radio Noise Filter")
	chkRadioNoise.SetChecked(audioProc.RadioNoiseEnabled)
	settingsWin.Add(chkRadioNoise)

	chkWalkieTalkie := wui.NewCheckBox()
	chkWalkieTalkie.SetBounds(740, 228, 180, 22)
	chkWalkieTalkie.SetText("📞 Walkie Talkie (VHF)")
	chkWalkieTalkie.SetChecked(audioProc.WalkieTalkieEnabled)
	settingsWin.Add(chkWalkieTalkie)

	chkDistortedAudio := wui.NewCheckBox()
	chkDistortedAudio.SetBounds(740, 256, 180, 22)
	chkDistortedAudio.SetText("💥 Blown-out Speaker")
	chkDistortedAudio.SetChecked(audioProc.DistortedAudioEnabled)
	settingsWin.Add(chkDistortedAudio)

	chkCompressor := wui.NewCheckBox()
	chkCompressor.SetBounds(740, 292, 200, 22)
	chkCompressor.SetText("─ Compressor ────────────────")
	chkCompressor.SetChecked(audioProc.CompressorEnabled)
	settingsWin.Add(chkCompressor)

	lblThresh := wui.NewLabel()
	lblThresh.SetBounds(740, 324, 110, 20)
	lblThresh.SetText("Threshold (dB):")
	settingsWin.Add(lblThresh)

	txtThresh := wui.NewEditLine()
	txtThresh.SetBounds(860, 322, 70, 22)
	txtThresh.SetText(fmt.Sprintf("%.1f", audioProc.CompressorThreshold))
	settingsWin.Add(txtThresh)

	lblRatio := wui.NewLabel()
	lblRatio.SetBounds(740, 352, 110, 20)
	lblRatio.SetText("Ratio (x:1):")
	settingsWin.Add(lblRatio)

	txtRatio := wui.NewEditLine()
	txtRatio.SetBounds(860, 350, 70, 22)
	txtRatio.SetText(fmt.Sprintf("%.1f", audioProc.CompressorRatio))
	settingsWin.Add(txtRatio)


	// Preset handlers
	btnHIZZER.SetOnClick(func() {
		chkBuzzer.SetChecked(true)
		txtBuzzerFreq.SetText("4625")
		txtBuzzerRate.SetText("1.0")
		txtBuzzerMod.SetText("0.8")
		
		chkLowPass.SetChecked(true)
		txtLowPass.SetText("3400")
		
		chkHighPass.SetChecked(true)
		txtHighPass.SetText("300")
		
		chkDist.SetChecked(true)
		txtDist.SetText("0.3")
		
		chkBitCrush.SetChecked(true)
		txtBitCrush.SetText("12")
		
		chkSampleRate.SetChecked(true)
		txtSampleRate.SetText("11025")
		
		chkEq.SetChecked(true)
		txtBass.SetText("-3")
		txtMid.SetText("2")
		txtTreble.SetText("-2")
		
		chkReverb.SetChecked(true)
		txtReverb.SetText("0.2")
		txtReverbDecay.SetText("1.5")
		
		chkDelay.SetChecked(true)
		txtDelay.SetText("0.15")
		txtDelayTime.SetText("0.25")
		txtDelayFb.SetText("0.3")
		
		chkNoiseFloor.SetChecked(true)
		txtNoise.SetText("-40")
		chkPulse.SetChecked(true)
		
		chkStaticCrackle.SetChecked(true)
		txtCrackle.SetText("0.1")
		
		chkWowFlutter.SetChecked(true)
		chkRadioFading.SetChecked(true)
		chkFilterSweep.SetChecked(false)
		
		chkCompressor.SetChecked(true)
		txtThresh.SetText("-12")
		txtRatio.SetText("4.0")
		
		chkRadioNoise.SetChecked(false)
		chkWalkieTalkie.SetChecked(false)
		chkDistortedAudio.SetChecked(false)

		chkRingMod.SetChecked(true)
		txtRingMod.SetText("50")
		chkAmpMod.SetChecked(true)
		txtAmpMod.SetText("2.0")
		chkPhaseMod.SetChecked(false)
		txtPhaseMod.SetText("1.0")

		txtBeepFreq.SetText("800")
		txtBeepVol.SetText("0.80")
		txtBeepDur.SetText("0.10")
		txtExtVol.SetText("0.80")
		txtExtVol2.SetText("0.80")
	})

	btnRadio.SetOnClick(func() {
		chkBuzzer.SetChecked(false)
		
		chkLowPass.SetChecked(true)
		txtLowPass.SetText("5000")
		
		chkHighPass.SetChecked(true)
		txtHighPass.SetText("100")
		
		chkDist.SetChecked(true)
		txtDist.SetText("0.05")
		
		chkBitCrush.SetChecked(false)
		txtBitCrush.SetText("16")
		
		chkSampleRate.SetChecked(false)
		txtSampleRate.SetText("44100")
		
		chkEq.SetChecked(true)
		txtBass.SetText("2")
		txtMid.SetText("0")
		txtTreble.SetText("-1")
		
		chkReverb.SetChecked(true)
		txtReverb.SetText("0.05")
		txtReverbDecay.SetText("1.0")
		
		chkDelay.SetChecked(false)
		txtDelay.SetText("0")
		
		chkNoiseFloor.SetChecked(true)
		txtNoise.SetText("-50")
		chkPulse.SetChecked(false)
		
		chkStaticCrackle.SetChecked(true)
		txtCrackle.SetText("0.02")
		
		chkWowFlutter.SetChecked(false)
		chkRadioFading.SetChecked(true)
		chkFilterSweep.SetChecked(false)
		
		chkCompressor.SetChecked(true)
		txtThresh.SetText("-15")
		txtRatio.SetText("2.0")
		
		chkRadioNoise.SetChecked(true)
		chkWalkieTalkie.SetChecked(false)
		chkDistortedAudio.SetChecked(false)

		chkRingMod.SetChecked(false)
		txtRingMod.SetText("0")
		chkAmpMod.SetChecked(false)
		txtAmpMod.SetText("0.0")
		chkPhaseMod.SetChecked(false)
		txtPhaseMod.SetText("1.0")

		txtBeepFreq.SetText("1000")
		txtBeepVol.SetText("0.60")
		txtBeepDur.SetText("0.08")
		txtExtVol.SetText("0.50")
		txtExtVol2.SetText("0.50")
	})

	btnLoFi.SetOnClick(func() {
		chkBuzzer.SetChecked(false)
		
		chkLowPass.SetChecked(true)
		txtLowPass.SetText("8000")
		
		chkHighPass.SetChecked(true)
		txtHighPass.SetText("50")
		
		chkDist.SetChecked(true)
		txtDist.SetText("0.4")
		
		chkBitCrush.SetChecked(true)
		txtBitCrush.SetText("8")
		
		chkSampleRate.SetChecked(true)
		txtSampleRate.SetText("22050")
		
		chkEq.SetChecked(true)
		txtBass.SetText("3")
		txtMid.SetText("1")
		txtTreble.SetText("-4")
		
		chkReverb.SetChecked(true)
		txtReverb.SetText("0.3")
		txtReverbDecay.SetText("2.0")
		
		chkDelay.SetChecked(true)
		txtDelay.SetText("0.25")
		txtDelayTime.SetText("0.3")
		txtDelayFb.SetText("0.4")
		
		chkNoiseFloor.SetChecked(true)
		txtNoise.SetText("-35")
		chkPulse.SetChecked(false)
		
		chkStaticCrackle.SetChecked(true)
		txtCrackle.SetText("0.15")
		
		chkWowFlutter.SetChecked(true)
		chkRadioFading.SetChecked(false)
		chkFilterSweep.SetChecked(false)
		
		chkCompressor.SetChecked(true)
		txtThresh.SetText("-10")
		txtRatio.SetText("3.0")
		
		chkRadioNoise.SetChecked(false)
		chkWalkieTalkie.SetChecked(false)
		chkDistortedAudio.SetChecked(false)

		chkRingMod.SetChecked(true)
		txtRingMod.SetText("30")
		chkAmpMod.SetChecked(true)
		txtAmpMod.SetText("3.0")
		chkPhaseMod.SetChecked(false)
		txtPhaseMod.SetText("1.0")

		txtBeepFreq.SetText("600")
		txtBeepVol.SetText("0.70")
		txtBeepDur.SetText("0.15")
		txtExtVol.SetText("0.90")
		txtExtVol2.SetText("0.90")
	})

	btnClean.SetOnClick(func() {
		chkBuzzer.SetChecked(false)
		
		chkLowPass.SetChecked(false)
		txtLowPass.SetText("20000")
		
		chkHighPass.SetChecked(false)
		txtHighPass.SetText("20")
		
		chkDist.SetChecked(false)
		txtDist.SetText("0")
		
		chkBitCrush.SetChecked(false)
		txtBitCrush.SetText("16")
		
		chkSampleRate.SetChecked(false)
		txtSampleRate.SetText("44100")
		
		chkEq.SetChecked(false)
		txtBass.SetText("0")
		txtMid.SetText("0")
		txtTreble.SetText("0")
		
		chkReverb.SetChecked(false)
		txtReverb.SetText("0")
		txtReverbDecay.SetText("1.0")
		
		chkDelay.SetChecked(false)
		txtDelay.SetText("0")
		
		chkNoiseFloor.SetChecked(false)
		txtNoise.SetText("-80")
		chkPulse.SetChecked(false)
		
		chkStaticCrackle.SetChecked(false)
		txtCrackle.SetText("0")
		
		chkWowFlutter.SetChecked(false)
		chkRadioFading.SetChecked(false)
		chkFilterSweep.SetChecked(false)
		
		chkCompressor.SetChecked(false)
		txtThresh.SetText("0")
		txtRatio.SetText("1.0")
		
		chkRadioNoise.SetChecked(false)
		chkWalkieTalkie.SetChecked(false)
		chkDistortedAudio.SetChecked(false)

		chkRingMod.SetChecked(false)
		txtRingMod.SetText("0")
		chkAmpMod.SetChecked(false)
		txtAmpMod.SetText("0.0")
		chkPhaseMod.SetChecked(false)
		txtPhaseMod.SetText("1.0")

		txtBeepFreq.SetText("800")
		txtBeepVol.SetText("1.00")
		txtBeepDur.SetText("0.10")
		txtExtVol.SetText("1.00")
		txtExtVol2.SetText("1.00")
	})

	sepFooter := wui.NewLabel()
	sepFooter.SetBounds(20, 445, 910, 2)
	sepFooter.SetText("──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────")
	settingsWin.Add(sepFooter)

	btnClose := wui.NewButton()
	btnClose.SetBounds(10, 452, 60, 30)
	btnClose.SetText("Apply")
	settingsWin.Add(btnClose)

	btnClose.SetOnClick(func() {
		// Apply settings
		audioProc.BuzzerEnabled = chkBuzzer.Checked()
		audioProc.BuzzerFrequency, _ = strconv.ParseFloat(txtBuzzerFreq.Text(), 64)
		audioProc.BuzzerPulseRate, _ = strconv.ParseFloat(txtBuzzerRate.Text(), 64)
		audioProc.BuzzerModDepth, _ = strconv.ParseFloat(txtBuzzerMod.Text(), 64)
		
		audioProc.LowPassEnabled = chkLowPass.Checked()
		audioProc.LowPassCutoff, _ = strconv.ParseFloat(txtLowPass.Text(), 64)
		
		audioProc.HighPassEnabled = chkHighPass.Checked()
		audioProc.HighPassCutoff, _ = strconv.ParseFloat(txtHighPass.Text(), 64)
		
		audioProc.BandPassEnabled = chkHighPass.Checked()
		
		audioProc.DistortionEnabled = chkDist.Checked()
		audioProc.DistortionAmount, _ = strconv.ParseFloat(txtDist.Text(), 64)
		
		audioProc.BitCrushEnabled = chkBitCrush.Checked()
		audioProc.BitCrushDepth, _ = strconv.Atoi(txtBitCrush.Text())
		
		audioProc.SampleRateReduceEnabled = chkSampleRate.Checked()
		audioProc.SampleRateReduce, _ = strconv.Atoi(txtSampleRate.Text())
		
		audioProc.EqEnabled = chkEq.Checked()
		audioProc.EqBassGain, _ = strconv.ParseFloat(txtBass.Text(), 64)
		audioProc.EqMidGain, _ = strconv.ParseFloat(txtMid.Text(), 64)
		audioProc.EqTrebleGain, _ = strconv.ParseFloat(txtTreble.Text(), 64)
		
		audioProc.ReverbEnabled = chkReverb.Checked()
		audioProc.ReverbAmount, _ = strconv.ParseFloat(txtReverb.Text(), 64)
		audioProc.ReverbDecay, _ = strconv.ParseFloat(txtReverbDecay.Text(), 64)
		
		audioProc.DelayEnabled = chkDelay.Checked()
		audioProc.DelayAmount, _ = strconv.ParseFloat(txtDelay.Text(), 64)
		audioProc.DelayTime, _ = strconv.ParseFloat(txtDelayTime.Text(), 64)
		audioProc.DelayFeedback, _ = strconv.ParseFloat(txtDelayFb.Text(), 64)
		
		audioProc.NoiseFloorEnabled = chkNoiseFloor.Checked()
		audioProc.NoiseFloor, _ = strconv.ParseFloat(txtNoise.Text(), 64)
		
		audioProc.PulseInterference = chkPulse.Checked()
		audioProc.WowFlutterEnabled = chkWowFlutter.Checked()
		audioProc.RadioFading = chkRadioFading.Checked()
		audioProc.FilterSweepEnabled = chkFilterSweep.Checked()
		
		audioProc.StaticCrackleEnabled = chkStaticCrackle.Checked()
		audioProc.StaticCrackle, _ = strconv.ParseFloat(txtCrackle.Text(), 64)
		
		audioProc.CompressorEnabled = chkCompressor.Checked()
		audioProc.CompressorThreshold, _ = strconv.ParseFloat(txtThresh.Text(), 64)
		audioProc.CompressorRatio, _ = strconv.ParseFloat(txtRatio.Text(), 64)
		
		audioProc.RadioNoiseEnabled = chkRadioNoise.Checked()
		audioProc.WalkieTalkieEnabled = chkWalkieTalkie.Checked()
		audioProc.DistortedAudioEnabled = chkDistortedAudio.Checked()

		audioProc.RingModEnabled = chkRingMod.Checked()
		audioProc.RingModFreq, _ = strconv.ParseFloat(txtRingMod.Text(), 64)

		audioProc.AmplitudeModEnabled = chkAmpMod.Checked()
		audioProc.AmplitudeModFreq, _ = strconv.ParseFloat(txtAmpMod.Text(), 64)

		audioProc.PhaseModEnabled = chkPhaseMod.Checked()
		audioProc.PhaseModFreq, _ = strconv.ParseFloat(txtPhaseMod.Text(), 64)

		audioProc.BeepFrequency, _ = strconv.ParseFloat(txtBeepFreq.Text(), 64)
		audioProc.BeepVolume, _ = strconv.ParseFloat(txtBeepVol.Text(), 64)
		audioProc.BeepDotDuration, _ = strconv.ParseFloat(txtBeepDur.Text(), 64)
		audioProc.ExternalAudioVolume, _ = strconv.ParseFloat(txtExtVol.Text(), 64)
		audioProc.ExternalAudio2Volume, _ = strconv.ParseFloat(txtExtVol2.Text(), 64)

		settingsWin.Close()
	})

	settingsWin.ShowModal()
}

func startLiveMicCapture() {
	liveMicMutex.Lock()
	defer liveMicMutex.Unlock()

	if liveMicCancel != nil {
		// Already running
		return
	}

	micDev := getDefaultDshowAudioDevice()
	if micDev == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	liveMicCancel = cancel
	liveMicData = make([]float64, 0, 4096)
	liveMicQueue = make([]float64, 0, 88200)

	cmd := exec.CommandContext(ctx, "ffmpeg", "-f", "dshow", "-i", "audio="+micDev, "-f", "s16le", "-ac", "1", "-ar", "44100", "-")
	liveMicCmd = cmd

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		liveMicCancel = nil
		return
	}

	if err := cmd.Start(); err != nil {
		cancel()
		liveMicCancel = nil
		return
	}

	go func() {
		reader := bufio.NewReader(stdoutPipe)
		var sampleBuf [2]byte

		for {
			_, err := io.ReadFull(reader, sampleBuf[:])
			if err != nil {
				break
			}
			val := int16(binary.LittleEndian.Uint16(sampleBuf[:]))
			sample := float64(val) / 32767.0

			liveMicMutex.Lock()
			liveMicData = append(liveMicData, sample)
			if len(liveMicData) > 4096 {
				liveMicData = liveMicData[len(liveMicData)-4096:]
			}
			liveMicQueue = append(liveMicQueue, sample)
			if len(liveMicQueue) > 88200 { // Limit queue to 2 seconds of buffer to prevent memory growth
				liveMicQueue = liveMicQueue[len(liveMicQueue)-88200:]
			}
			liveMicMutex.Unlock()
		}

		_ = cmd.Wait()
	}()
}

func stopLiveMicCapture() {
	liveMicMutex.Lock()
	defer liveMicMutex.Unlock()

	if liveMicCancel != nil {
		liveMicCancel()
		liveMicCancel = nil
	}
	liveMicCmd = nil
	liveMicData = nil
	liveMicQueue = nil
}

func getCharAmplitude(char rune) float64 {
	switch char {
	case ' ', '\t', '\r', '\n':
		return 0.0
	case '.', ',', '-', '_', ':':
		return 0.25
	case '=', '+', '*', 'i', 'l', '!':
		return 0.5
	case '%', '&', 'W', 'M', '#', '@':
		return 1.0
	default:
		return 0.8
	}
}

func generateAudioFromASCII(art string) []float64 {
	lines := strings.Split(art, "\n")
	var cleanedLines []string
	maxW := 0
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		cleanedLines = append(cleanedLines, line)
		if len(line) > maxW {
			maxW = len(line)
		}
	}

	if maxW == 0 {
		return make([]float64, sampleRate)
	}

	if maxW > 80 {
		maxW = 80
		for i, line := range cleanedLines {
			if len(line) > 80 {
				cleanedLines[i] = line[:80]
			}
		}
	}

	H := len(cleanedLines)
	W := maxW
	rowSamples := sampleRate / 20 // 50ms per row
	totalSamples := H * rowSamples
	samples := make([]float64, totalSamples)

	// Precompute frequencies and phase steps
	b_start := (80 - W) / 2
	freqs := make([]float64, W)
	phaseSteps := make([]float64, W)
	for c := 0; c < W; c++ {
		b := b_start + c
		minFreq := 20.0
		maxFreq := 12000.0
		freqs[c] = minFreq * math.Pow(maxFreq/minFreq, float64(b)/80.0)
		phaseSteps[c] = 2.0 * math.Pi * freqs[c] / float64(sampleRate)
	}

	// Setup amplitude matrix
	amps := make([][]float64, H)
	for r := 0; r < H; r++ {
		amps[r] = make([]float64, W)
		for c := 0; c < W; c++ {
			if c < len(cleanedLines[r]) {
				amps[r][c] = getCharAmplitude(rune(cleanedLines[r][c]))
			} else {
				amps[r][c] = 0.0
			}
		}
	}

	phases := make([]float64, W)

	// Generate samples with smooth spline linear amplitude interpolation
	for n := 0; n < totalSamples; n++ {
		i := n / rowSamples // Segment index
		r := i

		m := n % rowSamples
		p := float64(m) / float64(rowSamples)

		// Next and previous segments
		r_next := i + 1
		r_prev := i - 1

		var sum float64
		for c := 0; c < W; c++ {
			// FIXED: Use c directly without reversing (removed the reversed column index)
			A_curr := amps[r][c]

			var A_next float64
			if r_next < H {
				A_next = amps[r_next][c]
			}

			var A_prev float64
			if r_prev >= 0 {
				A_prev = amps[r_prev][c]
			}

			var targetAmp float64
			if p < 0.5 {
				targetAmp = A_prev*(0.5-p)*2.0 + A_curr*(p*2.0)
			} else {
				targetAmp = A_curr*((1.0-p)*2.0) + A_next*((p-0.5)*2.0)
			}

			sum += targetAmp * math.Sin(phases[c])

			// Keep phase running continuously
			phases[c] += phaseSteps[c]
			if phases[c] > 2.0*math.Pi {
				phases[c] -= 2.0 * math.Pi
			}
		}
		samples[n] = sum
	}

	// Normalize to prevent clipping (peak = 0.75)
	maxVal := 0.0
	for _, s := range samples {
		absVal := math.Abs(s)
		if absVal > maxVal {
			maxVal = absVal
		}
	}
	if maxVal > 0.0 {
		scale := 0.75 / maxVal
		for idx := range samples {
			samples[idx] *= scale
		}
	}

	return samples
}

func convertImageToASCII(filePath string, maxW, maxH int) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	targetW := maxW
	targetH := maxH

	// Maintain aspect ratio, but constrain bounds
	aspect := float64(w) / float64(h)
	if aspect > float64(targetW)/float64(targetH) {
		targetH = int(float64(targetW) / aspect)
	} else {
		targetW = int(float64(targetH) * aspect)
	}

	if targetW < 5 {
		targetW = 5
	}
	if targetH < 5 {
		targetH = 5
	}

	chars := []rune(" .:-=+*#%@")

	var sb strings.Builder
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			srcX := bounds.Min.X + x*w/targetW
			srcY := bounds.Min.Y + y*h/targetH

			color := img.At(srcX, srcY)
			r, g, b, _ := color.RGBA()

			// Grayscale conversion using luminance formula (0.0 to 1.0)
			gray := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 65535.0

			idx := int(gray * float64(len(chars)))
			if idx >= len(chars) {
				idx = len(chars) - 1
			}
			if idx < 0 {
				idx = 0
			}

			sb.WriteRune(chars[idx])
		}
		sb.WriteString("\r\n")
	}

	return sb.String(), nil
}

func showASCIIPainterDialog(
	parent *wui.Window,
	lblStatus *wui.Label,
	btnVisMode *wui.Button,
	btnPlayPause *wui.Button,
	btnStop *wui.Button,
	paintBox *wui.PaintBox,
	txtExternalAudio *wui.EditLine,
	txtExternalAudio2 *wui.EditLine,
	updateUI func(func()),
	refreshVisualizer func(),
) {
	editorWindow := wui.NewWindow()
	editorWindow.SetInnerPosition(parent.X()+40, parent.Y()+40)
	editorWindow.SetInnerWidth(800)
	editorWindow.SetInnerHeight(490)
	editorWindow.SetResizable(false)
	editorWindow.SetHasMaxButton(false)
	editorWindow.SetTitle("ASCII Spectrogram Painter")

	font, _ := wui.NewFont(wui.FontDesc{Name: "Segoe UI", Height: -12})
	editorWindow.SetFont(font)

	monoFont, _ := wui.NewFont(wui.FontDesc{Name: "Consolas", Height: -14})

	textEdit := wui.NewTextEdit()
	textEdit.SetBounds(10, 10, 780, 370)
	textEdit.SetWordWrap(false)
	textEdit.SetWritesTabs(true)
	textEdit.SetFont(monoFont)
	editorWindow.Add(textEdit)

	defaultArt := ".:-=+*#%@"
	textEdit.SetText(defaultArt)

	// ASCII conversion settings panel
	lblAsciiSettings := wui.NewLabel()
	lblAsciiSettings.SetBounds(10, 390, 150, 20)
	lblAsciiSettings.SetText("Image to ASCII Settings:")
	editorWindow.Add(lblAsciiSettings)

	lblAsciiWidth := wui.NewLabel()
	lblAsciiWidth.SetBounds(10, 415, 60, 20)
	lblAsciiWidth.SetText("Width:")
	editorWindow.Add(lblAsciiWidth)

	txtAsciiWidth := wui.NewEditLine()
	txtAsciiWidth.SetBounds(70, 413, 50, 24)
	txtAsciiWidth.SetText("70")
	editorWindow.Add(txtAsciiWidth)

	lblAsciiHeight := wui.NewLabel()
	lblAsciiHeight.SetBounds(140, 415, 60, 20)
	lblAsciiHeight.SetText("Height:")
	editorWindow.Add(lblAsciiHeight)

	txtAsciiHeight := wui.NewEditLine()
	txtAsciiHeight.SetBounds(200, 413, 50, 24)
	txtAsciiHeight.SetText("60")
	editorWindow.Add(txtAsciiHeight)

	lblAsciiNote := wui.NewLabel()
	lblAsciiNote.SetBounds(270, 415, 300, 20)
	lblAsciiNote.SetText("(Max 120x80 for best performance)")
	editorWindow.Add(lblAsciiNote)

	btnGenerate := wui.NewButton()
	btnGenerate.SetBounds(10, 450, 110, 32)
	btnGenerate.SetText("Generate")
	editorWindow.Add(btnGenerate)

	btnPlayStop := wui.NewButton()
	btnPlayStop.SetBounds(130, 450, 110, 32)
	btnPlayStop.SetText("Play")
	editorWindow.Add(btnPlayStop)

	btnGenExt1 := wui.NewButton()
	btnGenExt1.SetBounds(250, 450, 110, 32)
	btnGenExt1.SetText("Gen. Ext1")
	editorWindow.Add(btnGenExt1)

	btnGenExt2 := wui.NewButton()
	btnGenExt2.SetBounds(370, 450, 110, 32)
	btnGenExt2.SetText("Gen. Ext2")
	editorWindow.Add(btnGenExt2)

	btnImportImage := wui.NewButton()
	btnImportImage.SetBounds(490, 450, 140, 32)
	btnImportImage.SetText("Image to ASCII")
	editorWindow.Add(btnImportImage)

	btnClose := wui.NewButton()
	btnClose.SetBounds(640, 450, 150, 32)
	btnClose.SetText("Close")
	editorWindow.Add(btnClose)

	btnClose.SetOnClick(func() {
		editorWindow.Close()
	})

	btnImportImage.SetOnClick(func() {
		openDlg := wui.NewFileOpenDialog()
		openDlg.SetTitle("Open Image to Convert to ASCII")
		openDlg.AddFilter("Images (PNG, JPG)", "png", "jpg", "jpeg")
		ok, path := openDlg.ExecuteSingleSelection(editorWindow)
		if ok {
			// Parse width and height from input fields
			asciiWidth, err := strconv.Atoi(txtAsciiWidth.Text())
			if err != nil || asciiWidth < 10 {
				asciiWidth = 70
			}
			if asciiWidth > 120 {
				asciiWidth = 120
			}

			asciiHeight, err := strconv.Atoi(txtAsciiHeight.Text())
			if err != nil || asciiHeight < 5 {
				asciiHeight = 60
			}
			if asciiHeight > 80 {
				asciiHeight = 80
			}

			asciiArt, err := convertImageToASCII(path, asciiWidth, asciiHeight)
			if err != nil {
				wui.MessageBoxError("Conversion Error",
					"Failed to load or convert image: "+err.Error())
				return
			}
			textEdit.SetText(asciiArt)
		}
	})

	btnGenerate.SetOnClick(func() {
		art := textEdit.Text()
		if len(strings.TrimSpace(art)) == 0 {
			wui.MessageBox("Empty Canvas",
				"Please enter some ASCII art or import an image first!")
			return
		}

		btnGenerate.SetEnabled(false)
		lblStatus.SetText("Generating ASCII Art audio...")

		go func() {
			samples := generateAudioFromASCII(art)

			audioMutex.Lock()
			audioSamples = samples
			visualizerMode = 2 // Switch to Waterfall Spectrogram
			waveformDirty = true
			spectrumDirty = true
			waterfallDirty = true
			audioMutex.Unlock()

			updateUI(func() {
				btnGenerate.SetEnabled(true)
				btnVisMode.SetText("Waterfall")

				// Repaint and trigger visualizer recalculation
				paintBox.Paint()
				refreshVisualizer()

				lblStatus.SetText("ASCII Spectrogram audio generated successfully.")
			})
		}()
	})

	btnPlayStop.SetOnClick(func() {
		audioMutex.RLock()
		samples := audioSamples
		audioMutex.RUnlock()

		if len(samples) == 0 {
			wui.MessageBox("No Audio",
				"Please generate the ASCII art audio first!")
			return
		}

		if !playbackController.IsPlaying() {
			btnPlayStop.SetText("Stop")
			lblStatus.SetText("Playing ASCII Spectrogram audio...")
			btnPlayPause.SetText("Pause")
			btnStop.SetEnabled(true)

			playbackController.Play(samples, func() {
				updateUI(func() {
					btnPlayStop.SetText("Play")
					btnPlayPause.SetText("Play")
					btnStop.SetEnabled(false)
					lblStatus.SetText("ASCII Spectrogram playback completed.")
				})
			})
		} else {
			playbackController.Stop()
			updateUI(func() {
				btnPlayStop.SetText("Play")
				btnPlayPause.SetText("Play")
				btnStop.SetEnabled(false)
				lblStatus.SetText("Playback stopped.")
			})
		}
	})

	btnGenExt1.SetOnClick(func() {
		art := textEdit.Text()
		if len(strings.TrimSpace(art)) == 0 {
			wui.MessageBox("Empty Canvas",
				"Please enter some ASCII art or import an image first!")
			return
		}

		btnGenExt1.SetEnabled(false)
		lblStatus.SetText("Generating and saving WAV to External Audio 1...")

		go func() {
			samples := generateAudioFromASCII(art)

			// Save to wav
			err := writeWavFile("external_audio_1.wav", samples, sampleRate)

			audioMutex.Lock()
			audioSamples = samples
			visualizerMode = 2 // Switch to Waterfall Spectrogram
			waveformDirty = true
			spectrumDirty = true
			waterfallDirty = true
			audioMutex.Unlock()

			updateUI(func() {
				btnGenExt1.SetEnabled(true)
				btnVisMode.SetText("Waterfall")
				paintBox.Paint()
				refreshVisualizer()

				if err != nil {
					wui.MessageBoxError("Save Error",
						"Failed to save WAV file: "+err.Error())
					lblStatus.SetText("Failed to save External Audio 1.")
				} else {
					txtExternalAudio.SetText("external_audio_1.wav")
					lblStatus.SetText("Successfully generated and loaded into External Audio 1!")
				}
			})
		}()
	})

	btnGenExt2.SetOnClick(func() {
		art := textEdit.Text()
		if len(strings.TrimSpace(art)) == 0 {
			wui.MessageBox("Empty Canvas",
				"Please enter some ASCII art or import an image first!")
			return
		}

		btnGenExt2.SetEnabled(false)
		lblStatus.SetText("Generating and saving WAV to External Audio 2...")

		go func() {
			samples := generateAudioFromASCII(art)

			// Save to wav
			err := writeWavFile("external_audio_2.wav", samples, sampleRate)

			audioMutex.Lock()
			audioSamples = samples
			visualizerMode = 2 // Switch to Waterfall Spectrogram
			waveformDirty = true
			spectrumDirty = true
			waterfallDirty = true
			audioMutex.Unlock()

			updateUI(func() {
				btnGenExt2.SetEnabled(true)
				btnVisMode.SetText("Waterfall")
				paintBox.Paint()
				refreshVisualizer()

				if err != nil {
					wui.MessageBoxError("Save Error",
						"Failed to save WAV file: "+err.Error())
					lblStatus.SetText("Failed to save External Audio 2.")
				} else {
					txtExternalAudio2.SetText("external_audio_2.wav")
					lblStatus.SetText("Successfully generated and loaded into External Audio 2!")
				}
			})
		}()
	})

	editorWindow.ShowModal()
}

func runGUI() {
	appCfg := loadConfig()
	appCfg.UseLiveMic = false
	*audioProc = appCfg.Processor
	visualizerMode = appCfg.VisualizerMode

	_ = speaker.Init(beepFormat.SampleRate, beepFormat.SampleRate.N(time.Second/10))
	
	playbackController = NewPlaybackController()

	font, _ := wui.NewFont(wui.FontDesc{Name: "Segoe UI", Height: -12})
	window := wui.NewWindow()
	window.SetInnerPosition(30, 50)
	window.SetInnerWidth(950)
	window.SetInnerHeight(490)
	window.SetResizable(false)
	window.SetHasMaxButton(false)
	window.SetFont(font)
	window.SetTitle("The Hizzer")

	lblStatus := wui.NewLabel()

	// Input Labels and Edit Fields
	lblMsg := wui.NewLabel()
	lblMsg.SetBounds(20, 15, 110, 20)
	lblMsg.SetText("Morse Message:")
	window.Add(lblMsg)

	txtMsg := wui.NewEditLine()
	txtMsg.SetBounds(140, 13, 330, 24)
	txtMsg.SetText(appCfg.MorseMessage)
	window.Add(txtMsg)

	lblBg := wui.NewLabel()
	lblBg.SetBounds(20, 45, 110, 20)
	lblBg.SetText("Background Src:")
	window.Add(lblBg)

	txtBg := wui.NewEditLine()
	txtBg.SetBounds(140, 43, 260, 24)
	txtBg.SetText(appCfg.BackgroundImage)
	window.Add(txtBg)

	btnBrowseBg := wui.NewButton()
	btnBrowseBg.SetBounds(405, 42, 65, 26)
	btnBrowseBg.SetText("Browse")
	window.Add(btnBrowseBg)

	btnBrowseBg.SetOnClick(func() {
		openDlg := wui.NewFileOpenDialog()
		openDlg.SetTitle("Open Background Image or Video")
		openDlg.AddFilter("All Supported Files", "jpg", "jpeg", "png", "mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "m4v")
		openDlg.AddFilter("Images (PNG, JPG)", "jpg", "jpeg", "png")
		openDlg.AddFilter("Videos (MP4, MKV, AVI, etc.)", "mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "m4v")
		ok, path := openDlg.ExecuteSingleSelection(window)
		if ok {
			txtBg.SetText(path)
		}
	})

	lblRtmp := wui.NewLabel()
	lblRtmp.SetBounds(500, 15, 100, 20)
	lblRtmp.SetText("RTMP Endpoint:")
	window.Add(lblRtmp)

	txtRtmp := wui.NewEditLine()
	txtRtmp.SetBounds(610, 13, 320, 24)
	txtRtmp.SetText(appCfg.RTMPURL)
	window.Add(txtRtmp)

	lblOut := wui.NewLabel()
	lblOut.SetBounds(500, 45, 100, 20)
	lblOut.SetText("Local Output:")
	window.Add(lblOut)

	txtOut := wui.NewEditLine()
	txtOut.SetBounds(610, 43, 320, 24)
	txtOut.SetText(appCfg.OutputFile)
	window.Add(txtOut)

	lblExternalAudio := wui.NewLabel()
	lblExternalAudio.SetBounds(20, 75, 110, 20)
	lblExternalAudio.SetText("External Audio:")
	window.Add(lblExternalAudio)

	txtExternalAudio := wui.NewEditLine()
	txtExternalAudio.SetBounds(140, 73, 190, 24)
	txtExternalAudio.SetText(appCfg.ExternalAudio)
	window.Add(txtExternalAudio)

	btnBrowseAudio := wui.NewButton()
	btnBrowseAudio.SetBounds(335, 72, 65, 26)
	btnBrowseAudio.SetText("Browse")
	window.Add(btnBrowseAudio)

	btnBrowseAudio.SetOnClick(func() {
		openDlg := wui.NewFileOpenDialog()
		openDlg.SetTitle("Open External WAV Audio File")
		openDlg.AddFilter("WAV Audio Files", "wav")
		ok, path := openDlg.ExecuteSingleSelection(window)
		if ok {
			txtExternalAudio.SetText(path)
		}
	})

	btnRecordMic := wui.NewButton()
	btnRecordMic.SetBounds(405, 72, 65, 26)
	btnRecordMic.SetText("Record")
	window.Add(btnRecordMic)

	
	lblExternalAudio2 := wui.NewLabel()
	lblExternalAudio2.SetBounds(20, 105, 110, 20)
	lblExternalAudio2.SetText("Secondary Audio:")
	window.Add(lblExternalAudio2)

	txtExternalAudio2 := wui.NewEditLine()
	txtExternalAudio2.SetBounds(140, 103, 260, 24)
	txtExternalAudio2.SetText(appCfg.ExternalAudio2)
	window.Add(txtExternalAudio2)

	btnBrowseAudio2 := wui.NewButton()
	btnBrowseAudio2.SetBounds(405, 102, 65, 26)
	btnBrowseAudio2.SetText("Browse")
	window.Add(btnBrowseAudio2)

	btnBrowseAudio2.SetOnClick(func() {
		openDlg := wui.NewFileOpenDialog()
		openDlg.SetTitle("Open Secondary WAV Audio File")
		openDlg.AddFilter("WAV Audio Files", "wav")
		ok, path := openDlg.ExecuteSingleSelection(window)
		if ok {
			txtExternalAudio2.SetText(path)
		}
	})

	lblDur := wui.NewLabel()
	lblDur.SetBounds(500, 75, 100, 20)
	lblDur.SetText("Duration (Secs):")
	window.Add(lblDur)

	txtDur := wui.NewEditLine()
	txtDur.SetBounds(610, 73, 60, 24)
	txtDur.SetText(fmt.Sprintf("%d", appCfg.Duration))
	window.Add(txtDur)

	chkLiveUpdate := wui.NewCheckBox()
	chkLiveUpdate.SetBounds(680, 74, 100, 22)
	chkLiveUpdate.SetText("Live Update")
	chkLiveUpdate.SetChecked(true)
	window.Add(chkLiveUpdate)

	btnStartLiveMic := wui.NewButton()
	btnStartLiveMic.SetBounds(790, 72, 70, 26)
	btnStartLiveMic.SetText("🎙️ Start")
	window.Add(btnStartLiveMic)

	btnStopLiveMic := wui.NewButton()
	btnStopLiveMic.SetBounds(865, 72, 65, 26)
	btnStopLiveMic.SetText("🛑 Stop")
	btnStopLiveMic.SetEnabled(false)
	window.Add(btnStopLiveMic)

	// Initialize button state based on appCfg
	if appCfg.UseLiveMic {
		startLiveMicCapture()
		btnStartLiveMic.SetEnabled(false)
		btnStopLiveMic.SetEnabled(true)
	} else {
		btnStartLiveMic.SetEnabled(true)
		btnStopLiveMic.SetEnabled(false)
	}

	btnStartLiveMic.SetOnClick(func() {
		startLiveMicCapture()
		btnStartLiveMic.SetEnabled(false)
		btnStopLiveMic.SetEnabled(true)
		appCfg.UseLiveMic = true
		
		// Save config
		var durSec int
		fmt.Sscanf(txtDur.Text(), "%d", &durSec)
		saveConfig(AppConfig{
			Processor:       *audioProc,
			MorseMessage:    txtMsg.Text(),
			BackgroundImage: txtBg.Text(),
			RTMPURL:         txtRtmp.Text(),
			OutputFile:      txtOut.Text(),
			ExternalAudio:   txtExternalAudio.Text(),
			ExternalAudio2:  txtExternalAudio2.Text(),
			Duration:        durSec,
			VisualizerMode:  visualizerMode,
			UseLiveMic:      true,
		})
		lblStatus.SetText("Live Mic started.")
	})

	btnStopLiveMic.SetOnClick(func() {
		stopLiveMicCapture()
		btnStartLiveMic.SetEnabled(true)
		btnStopLiveMic.SetEnabled(false)
		appCfg.UseLiveMic = false
		
		// Save config
		var durSec int
		fmt.Sscanf(txtDur.Text(), "%d", &durSec)
		saveConfig(AppConfig{
			Processor:       *audioProc,
			MorseMessage:    txtMsg.Text(),
			BackgroundImage: txtBg.Text(),
			RTMPURL:         txtRtmp.Text(),
			OutputFile:      txtOut.Text(),
			ExternalAudio:   txtExternalAudio.Text(),
			ExternalAudio2:  txtExternalAudio2.Text(),
			Duration:        durSec,
			VisualizerMode:  visualizerMode,
			UseLiveMic:      false,
		})
		lblStatus.SetText("Live Mic stopped.")
	})

	btnGenerate := wui.NewButton()
	btnGenerate.SetBounds(500, 103, 140, 26)
	btnGenerate.SetText("Generate Audio")
	window.Add(btnGenerate)

	btnAudioSettings := wui.NewButton()
	btnAudioSettings.SetBounds(650, 103, 140, 26)
	btnAudioSettings.SetText("Settings")
	window.Add(btnAudioSettings)

	btnASCIIPainter := wui.NewButton()
	btnASCIIPainter.SetBounds(800, 103, 130, 26)
	btnASCIIPainter.SetText("ASCII Paint")
	window.Add(btnASCIIPainter)

	paintBox := wui.NewPaintBox()
	paintBox.SetBounds(20, 135, 680, 290)
	window.Add(paintBox)
	globalPaintBox = paintBox

	secPaintBox := wui.NewPaintBox()
	secPaintBox.SetBounds(710, 135, 220, 290)
	window.Add(secPaintBox)

	secPaintBox.SetOnPaint(func(canvas *wui.Canvas) {
		w, h := secPaintBox.Width(), secPaintBox.Height()
		
		// Background
		canvas.FillRect(0, 0, w, h, wui.RGB(8, 12, 18))
		
		// division line
		canvas.Line(0, h/2, w, h/2, wui.RGB(20, 28, 40))
		
		// --- TOP HALF: LIVE MIC ---
		centerY1 := h / 4
		canvas.Line(0, centerY1, w, centerY1, wui.RGB(15, 22, 32))
		canvas.TextOut(8, 6, "LIVE MIC", wui.RGB(80, 100, 120))
		
		liveMicMutex.RLock()
		micActive := liveMicCancel != nil
		var micSamples []float64
		if len(liveMicData) > 0 {
			micSamples = make([]float64, len(liveMicData))
			copy(micSamples, liveMicData)
		}
		liveMicMutex.RUnlock()
		
		if micActive && len(micSamples) > 0 {
			numDraw := 1000
			if len(micSamples) < numDraw {
				numDraw = len(micSamples)
			}
			drawData := micSamples[len(micSamples)-numDraw:]
			
			step := float64(numDraw) / float64(w)
			for i := 0; i < w-1; i++ {
				idx1 := int(float64(i) * step)
				idx2 := int(float64(i+1) * step)
				if idx2 >= len(drawData) {
					idx2 = len(drawData) - 1
				}
				
				maxAmp := 0.0
				for idx := idx1; idx <= idx2; idx++ {
					if absVal := math.Abs(drawData[idx]); absVal > maxAmp {
						maxAmp = absVal
					}
				}
				
				lineHeight := int(maxAmp * float64(centerY1-5))
				y1 := centerY1 - lineHeight
				y2 := centerY1 + lineHeight
				canvas.Line(i, y1, i, y2, wui.RGB(0, 229, 163))
			}
		} else {
			canvas.TextOut(w/2-30, centerY1-8, "OFFLINE", wui.RGB(100, 110, 120))
		}
		
		// --- BOTTOM HALF: BROADCAST ---
		centerY2 := 3 * h / 4
		canvas.Line(0, centerY2, w, centerY2, wui.RGB(15, 22, 32))
		canvas.TextOut(8, h/2+6, "BROADCAST", wui.RGB(80, 100, 120))
		
		broadcastMutex.RLock()
		var bcastSamples []float64
		if len(broadcastData) > 0 {
			bcastSamples = make([]float64, len(broadcastData))
			copy(bcastSamples, broadcastData)
		}
		broadcastMutex.RUnlock()
		
		activeBcast := playbackController.IsPlaying() || isStreaming
		
		if activeBcast && len(bcastSamples) > 0 {
			numDraw := 1000
			if len(bcastSamples) < numDraw {
				numDraw = len(bcastSamples)
			}
			drawData := bcastSamples[len(bcastSamples)-numDraw:]
			
			step := float64(numDraw) / float64(w)
			for i := 0; i < w-1; i++ {
				idx1 := int(float64(i) * step)
				idx2 := int(float64(i+1) * step)
				if idx2 >= len(drawData) {
					idx2 = len(drawData) - 1
				}
				
				maxAmp := 0.0
				for idx := idx1; idx <= idx2; idx++ {
					if absVal := math.Abs(drawData[idx]); absVal > maxAmp {
						maxAmp = absVal
					}
				}
				
				lineHeight := int(maxAmp * float64(h/4-5))
				y1 := centerY2 - lineHeight
				y2 := centerY2 + lineHeight
				canvas.Line(i, y1, i, y2, wui.RGB(255, 128, 0))
			}
		} else {
			canvas.TextOut(w/2-30, centerY2-8, "STANDBY", wui.RGB(100, 110, 120))
		}
	})

	btnPlayPause := wui.NewButton()
	btnPlayPause.SetBounds(20, 430, 60, 30)
	btnPlayPause.SetText("Play")
	window.Add(btnPlayPause)
	
	btnStop := wui.NewButton()
	btnStop.SetBounds(90, 430, 60, 30)
	btnStop.SetText("Stop")
	btnStop.SetEnabled(false)
	window.Add(btnStop)

	btnZoomIn := wui.NewButton()
	btnZoomIn.SetBounds(160, 430, 60, 30)
	btnZoomIn.SetText("Zoom (+)")
	window.Add(btnZoomIn)

	btnZoomOut := wui.NewButton()
	btnZoomOut.SetBounds(230, 430, 60, 30)
	btnZoomOut.SetText("Zoom (-)")
	window.Add(btnZoomOut)

	btnResetZoom := wui.NewButton()
	btnResetZoom.SetBounds(300, 430, 60, 30)
	btnResetZoom.SetText("Reset")
	window.Add(btnResetZoom)

	btnVisMode := wui.NewButton()
	btnVisMode.SetBounds(370, 430, 80, 30)
	if visualizerMode == 0 {
		btnVisMode.SetText("Waveform")
	} else if visualizerMode == 1 {
		btnVisMode.SetText("Spectrum")
	} else {
		btnVisMode.SetText("Waterfall")
	}
	window.Add(btnVisMode)

	btnStream := wui.NewButton()
	btnStream.SetBounds(830, 430, 100, 30)
	btnStream.SetText("Start Broadcast")
	btnStream.SetFont(font)
	window.Add(btnStream)

	lblStatus.SetBounds(20, 465, 800, 15)
	lblStatus.SetText("System Ready. Configure audio effects and generate Morse payload.")
	window.Add(lblStatus)

	updateUI := func(updateFunc func()) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			updateFunc()
		}()
	}

// Smart updater: Only calculates the active visualizer, and does it in a background thread
	refreshVisualizer := func() {
		mode := visualizerMode
		w := paintBox.Width()
		
		go func() {
			audioMutex.RLock()
			samples := audioSamples
			audioMutex.RUnlock()

			if len(samples) == 0 {
				return
			}

			needRepaint := false

			if mode == 0 && waveformDirty {
				wf := computeWaveform(samples, w)
				audioMutex.Lock()
				waveformData = wf
				waveformDirty = false
				audioMutex.Unlock()
				needRepaint = true
			} else if mode == 1 && spectrumDirty {
				sp := computeSpectrum(samples, 60)
				audioMutex.Lock()
				spectrumData = sp
				spectrumPeaks = make([]float64, 60)
				copy(spectrumPeaks, sp)
				spectrumDirty = false
				audioMutex.Unlock()
				needRepaint = true
			} else if mode == 2 && waterfallDirty {
				wf := computeWaterfall(samples, waterfallRows, waterfallBins)
				audioMutex.Lock()
				waterfallData = wf
				waterfallDirty = false
				audioMutex.Unlock()
				needRepaint = true
			}

			if needRepaint {
				updateUI(func() { paintBox.Paint() })
			}
		}()
	}

audioSamples = generateAudioBuffers(textToMorse(txtMsg.Text()), strings.TrimSpace(txtExternalAudio.Text()), strings.TrimSpace(txtExternalAudio2.Text()))
	// waveformData = computeWaveform(audioSamples, paintBox.Width())
	// spectrumData = computeSpectrum(audioSamples, 60)
	// waterfallData = computeWaterfall(audioSamples, waterfallRows, waterfallBins)
	// spectrumPeaks = make([]float64, 60)
	// copy(spectrumPeaks, spectrumData)
	waveformDirty = true
	spectrumDirty = true
	waterfallDirty = true
	refreshVisualizer() // Kick off the smart calculation

	generateAudio := func() {
		btnGenerate.SetEnabled(false)
		lblStatus.SetText("Generating audio with effects... Please wait.")
		
		go func() {
			morseStr := textToMorse(txtMsg.Text())
			newSamples := generateAudioBuffers(morseStr, strings.TrimSpace(txtExternalAudio.Text()), strings.TrimSpace(txtExternalAudio2.Text()))
			
			updateUI(func() {
				audioMutex.Lock()
				audioSamples = newSamples
				if chkLiveUpdate.Checked() {
					audioVersion++
				}
				
				// 1. Force clear the active visualizer data to trigger the "Generating..." loading text
				if visualizerMode == 0 {
					waveformData = nil
				} else if visualizerMode == 1 {
					spectrumData = nil
				} else if visualizerMode == 2 {
					waterfallData = nil
				}
				
				// 2. Mark all visualizers as needing recalculation
				waveformDirty = true
				spectrumDirty = true
				waterfallDirty = true
				audioMutex.Unlock()
				
				// 3. Paint immediately to show the blank / loading state
				paintBox.Paint()
				
				// 4. Kick off the background calculation (it will repaint when finished)
				refreshVisualizer() 

				btnGenerate.SetEnabled(true)
				lblStatus.SetText("Audio generated. Total duration: " + fmt.Sprintf("%.2f", float64(len(audioSamples))/float64(sampleRate)) + " seconds")

				var durSec int
				fmt.Sscanf(txtDur.Text(), "%d", &durSec)
				saveConfig(AppConfig{
					Processor:       *audioProc,
					MorseMessage:    txtMsg.Text(),
					BackgroundImage: txtBg.Text(),
					RTMPURL:         txtRtmp.Text(),
					OutputFile:      txtOut.Text(),
					ExternalAudio:   txtExternalAudio.Text(),
					ExternalAudio2:  txtExternalAudio2.Text(),
					Duration:        durSec,
					VisualizerMode:  visualizerMode,
					UseLiveMic:      appCfg.UseLiveMic,
				})
			})
		}()
	}

	btnGenerate.SetOnClick(generateAudio)

	btnASCIIPainter.SetOnClick(func() {
		showASCIIPainterDialog(
			window,
			lblStatus,
			btnVisMode,
			btnPlayPause,
			btnStop,
			paintBox,
			txtExternalAudio,
			txtExternalAudio2,
			updateUI,
			refreshVisualizer,
		)
	})

	btnAudioSettings.SetOnClick(func() {
		showAudioSettingsDialog(window)
		// Save config without regenerating audio
		var durSec int
		fmt.Sscanf(txtDur.Text(), "%d", &durSec)
		saveConfig(AppConfig{
			Processor:       *audioProc,
			MorseMessage:    txtMsg.Text(),
			BackgroundImage: txtBg.Text(),
			RTMPURL:         txtRtmp.Text(),
			OutputFile:      txtOut.Text(),
			ExternalAudio:   txtExternalAudio.Text(),
			ExternalAudio2:  txtExternalAudio2.Text(),
			Duration:        durSec,
			VisualizerMode:  visualizerMode,
			UseLiveMic:      appCfg.UseLiveMic,
		})
	})

	paintBox.SetOnPaint(func(canvas *wui.Canvas) {
		w, h := paintBox.Width(), paintBox.Height()
		canvas.FillRect(0, 0, w, h, wui.RGB(8, 12, 18))

		if visualizerMode == 0 {
			// Waveform mode
			centerY := h / 2
			audioMutex.RLock()
			currentWaveform := waveformData
			audioMutex.RUnlock()
			
			if len(currentWaveform) == 0 {
				return
			}

			// Draw grid background
			for i := 1; i < 10; i++ {
				gx := i * w / 10
				canvas.Line(gx, 0, gx, h, wui.RGB(20, 25, 35))
			}
			canvas.Line(0, centerY, w, centerY, wui.RGB(40, 50, 65))

			for i := 0; i < len(currentWaveform)-1; i++ {
				x1 := int(float64(i) / float64(len(currentWaveform)) * float64(w))
				y1 := centerY - int(currentWaveform[i]*float64(centerY-20))
				x2 := int(float64(i+1) / float64(len(currentWaveform)) * float64(w))
				y2 := centerY - int(currentWaveform[i+1]*float64(centerY-20))

				if x2 >= w {
					x2 = w - 1
				}

				amp := int((math.Abs(currentWaveform[i])) * 255)
				if amp > 255 {
					amp = 255
				}
				color := wui.RGB(uint8(amp/3), uint8(200-amp/4), uint8(amp/2))
				canvas.Line(x1, y1, x2, y2, color)
			}
		} else if visualizerMode == 1 {
			// Spectrum analyzer mode (existing)
			audioMutex.RLock()
			currentSpectrum := spectrumData
			currentPeaks := spectrumPeaks
			audioMutex.RUnlock()

			if len(currentSpectrum) == 0 {
				return
			}

			// Draw dB grids
			dbLevels := []float64{0.25, 0.5, 0.75}
			dbLabels := []string{"-24 dB", "-12 dB", "-6 dB"}
			for idx, lvl := range dbLevels {
				gy := h - int(lvl*float64(h-40)) - 20
				canvas.Line(0, gy, w, gy, wui.RGB(20, 28, 40))
				canvas.TextOut(10, gy-12, dbLabels[idx], wui.RGB(80, 100, 130))
			}

			// Draw frequency bands
			freqLabels := []struct {
				val  string
				xPct float64
			}{
				{"100 Hz", 0.15},
				{"500 Hz", 0.35},
				{"1 kHz", 0.55},
				{"5 kHz", 0.75},
				{"10 kHz", 0.90},
			}
			for _, fl := range freqLabels {
				fx := int(fl.xPct * float64(w))
				canvas.Line(fx, 0, fx, h-20, wui.RGB(20, 28, 40))
				canvas.TextOut(fx-15, h-18, fl.val, wui.RGB(80, 100, 130))
			}

			// Draw spectrum bars
			numBars := len(currentSpectrum)
			barWidth := (w - 40) / numBars
			spacing := 2

			for i := 0; i < numBars; i++ {
				barHeight := int(currentSpectrum[i] * float64(h-60))
				if barHeight < 2 {
					barHeight = 2
				}
				bx := 20 + i*(barWidth+spacing)

				// Draw gradient bar
				for j := 0; j < barHeight; j++ {
					pct := float64(j) / float64(h-60)
					r := uint8(255 * pct)
					g := uint8(100 + 155*(1.0-pct))
					b := uint8(255)
					canvas.FillRect(bx, h-25-j, bx+barWidth, h-25-j+1, wui.RGB(r, g, b))
				}

				// Draw peak hold dot
				peakY := h - 25 - int(currentPeaks[i]*float64(h-60))
				canvas.FillRect(bx, peakY, bx+barWidth, peakY+2, wui.RGB(255, 120, 0))

				// Slowly decay peak hold
				if currentPeaks[i] > currentSpectrum[i] {
					currentPeaks[i] -= 0.005
				} else {
					currentPeaks[i] = currentSpectrum[i]
				}
			}
		} else if visualizerMode == 2 {
			// Waterfall spectrogram mode
			audioMutex.RLock()
			currentWaterfall := waterfallData
			audioMutex.RUnlock()

			if len(currentWaterfall) == 0 {
				canvas.TextOut(w/2-100, h/2, "Generating waterfall data...", wui.RGB(150, 150, 150))
				return
			}

			// Draw waterfall visualization
			rowHeight := float64(h) / float64(len(currentWaterfall))
			
			for r := 0; r < len(currentWaterfall); r++ {
				y1 := int(float64(r) * rowHeight)
				y2 := int(float64(r+1) * rowHeight)
				if y2 > h {
					y2 = h
				}
				
				row := currentWaterfall[r]
				binWidth := float64(w) / float64(len(row))
				
				for b := 0; b < len(row); b++ {
					x1 := int(float64(b) * binWidth)
					x2 := int(float64(b+1) * binWidth)
					if x2 > w {
						x2 = w
					}
					
					color := getWaterfallColor(row[b])
					canvas.FillRect(x1, y1, x2, y2, color)
				}
			}
			
			// Draw frequency labels on the top
			freqPositions := []struct {
				freq string
				bin  int
			}{
				{"50", 2},
				{"100", 4},
				{"500", 10},
				{"1k", 20},
				{"2k", 30},
				{"5k", 45},
				{"10k", 65},
			}
			
			for _, fp := range freqPositions {
				if fp.bin < waterfallBins {
					x := int(float64(fp.bin) * float64(w) / float64(waterfallBins))
					canvas.TextOut(x-15, 2, fp.freq, wui.RGB(200, 200, 200))
				}
			}
			
			// Draw time labels on the right
			timeLabels := []struct {
				label string
				row   int
			}{
				{"Now", 0},
				{"-1s", waterfallRows / 4},
				{"-2s", waterfallRows / 2},
				{"-3s", 3 * waterfallRows / 4},
			}
			
			for _, tl := range timeLabels {
				if tl.row < len(currentWaterfall) {
					y := int(float64(tl.row) * rowHeight)
					if y+15 < h {
						canvas.TextOut(w-40, y, tl.label, wui.RGB(150, 150, 150))
					}
				}
			}
		}
	})

	btnVisMode.SetOnClick(func() {
		if visualizerMode == 0 {
			visualizerMode = 1
			btnVisMode.SetText("Spectrum")
		} else if visualizerMode == 1 {
			visualizerMode = 2
			btnVisMode.SetText("Waterfall")
		} else {
			visualizerMode = 0
			btnVisMode.SetText("Waveform")
		}
		
		paintBox.Paint()      // Repaint immediately to clear old visualizer
		refreshVisualizer()   // Calculate the new visualizer if it's dirty
		
		var durSec int
		fmt.Sscanf(txtDur.Text(), "%d", &durSec)
		saveConfig(AppConfig{
			Processor:       *audioProc,
			MorseMessage:    txtMsg.Text(),
			BackgroundImage: txtBg.Text(),
			RTMPURL:         txtRtmp.Text(),
			OutputFile:      txtOut.Text(),
			ExternalAudio:   txtExternalAudio.Text(),
			ExternalAudio2:  txtExternalAudio2.Text(),
			Duration:        durSec,
			VisualizerMode:  visualizerMode,
			UseLiveMic:      appCfg.UseLiveMic,
		})
	})

	btnZoomIn.SetOnClick(func() {
		zoomLevel *= 1.3
		if zoomLevel > 20 {
			zoomLevel = 20
		}
		waveformDirty = true
		refreshVisualizer()
	})
	
	btnZoomOut.SetOnClick(func() {
		zoomLevel /= 1.3
		if zoomLevel < 1.0 {
			zoomLevel = 1.0
		}
		waveformDirty = true
		refreshVisualizer()
	})

	btnResetZoom.SetOnClick(func() {
		zoomLevel = 1.0
		zoomOffset = 0.0
		waveformData = computeWaveform(audioSamples, paintBox.Width())
		paintBox.Paint()
	})

	btnPlayPause.SetOnClick(func() {
		audioMutex.RLock()
		samples := audioSamples
		audioMutex.RUnlock()
		
		if len(samples) == 0 {
			lblStatus.SetText("No audio generated yet. Please generate audio first.")
			return
		}
		
		if !playbackController.IsPlaying() {
			btnStop.SetEnabled(true)
			lblStatus.SetText("Playing audio...")
			btnPlayPause.SetText("Pause")
			
			playbackController.Play(samples, func() {
				updateUI(func() {
					btnPlayPause.SetText("Play")
					btnStop.SetEnabled(false)
					lblStatus.SetText("Playback completed.")
				})
			})
		} else {
			if playbackController.IsPaused() {
				playbackController.Resume()
				btnPlayPause.SetText("Pause")
				lblStatus.SetText("Playback resumed.")
			} else {
				playbackController.Pause()
				btnPlayPause.SetText("Resume")
				lblStatus.SetText("Playback paused.")
			}
		}
	})
	
	btnStop.SetOnClick(func() {
		if playbackController.IsPlaying() {
			playbackController.Stop()
			btnPlayPause.SetText("Play")
			btnStop.SetEnabled(false)
			lblStatus.SetText("Playback stopped.")
		}
	})

	var lastMouseX int
	isMouseDown := false
	paintBox.SetOnMouseMove(func(x, y int) {
		if isMouseDown {
			zoomOffset += float64(x-lastMouseX) / 2
			if zoomOffset < 0 {
				zoomOffset = 0
			}
			waveformData = computeWaveform(audioSamples, paintBox.Width())
			lastMouseX = x
			paintBox.Paint()
		}
	})

	window.SetOnMouseDown(func(mb wui.MouseButton, x int, _ int) {
		if mb == wui.MouseButtonLeft {
			isMouseDown = true
			lastMouseX = x
		}
	})

	window.SetOnMouseUp(func(mb wui.MouseButton, _ int, _ int) {
		if mb == wui.MouseButtonLeft {
			isMouseDown = false
		}
	})

	btnStream.SetOnClick(func() {
		if isStreaming {
			if streamCancel != nil {
				streamCancel()
			}
			return
		}

		bgPath := strings.TrimSpace(txtBg.Text())

		var durSec int
		fmt.Sscanf(txtDur.Text(), "%d", &durSec)
		saveConfig(AppConfig{
			Processor:       *audioProc,
			MorseMessage:    txtMsg.Text(),
			BackgroundImage: txtBg.Text(),
			RTMPURL:         txtRtmp.Text(),
			OutputFile:      txtOut.Text(),
			ExternalAudio:   txtExternalAudio.Text(),
			ExternalAudio2:  txtExternalAudio2.Text(),
			Duration:        durSec,
			VisualizerMode:  visualizerMode,
			UseLiveMic:      appCfg.UseLiveMic,
		})

		if _, err := os.Stat(bgPath); os.IsNotExist(err) {
			lblStatus.SetText("Warning: Background not found. Creating generic black layout...")
			if err := createDefaultBackground(bgPath); err != nil {
				wui.MessageBoxError("Error", "Could not create background")
				return
			}
		}

		cfg := Config{
			BackgroundImage: bgPath,
			RTMPURL:         strings.TrimSpace(txtRtmp.Text()),
			MorseCode:       txtMsg.Text(),
			FPS:             30,
			VideoBitrate:    "3000k",
			AudioBitrate:    "128k",
			Duration:        durSec,
			OutputFile:      strings.TrimSpace(txtOut.Text()),
			UseLiveMic:      appCfg.UseLiveMic,
		}

		if cfg.RTMPURL == "" && cfg.OutputFile == "" {
			wui.MessageBoxError("Error", "Please specify RTMP URL or output file")
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		streamCancel = cancel
		isStreaming = true

		btnStream.SetText("Stop Broadcast")
		lblStatus.SetText("Broadcasting...")

		go func() {
			for {
				select {
				case <-ctx.Done():
					updateUI(func() {
						isStreaming = false
						lblStatus.SetText("Broadcast stopped.")
						btnStream.SetText("Start Broadcast")
					})
					return
				default:
					err := executeStreamPipeline(ctx, cfg)
					
					if ctx.Err() != nil {
						updateUI(func() {
							isStreaming = false
							lblStatus.SetText("Broadcast stopped.")
							btnStream.SetText("Start Broadcast")
						})
						return
					}
					
					if err != nil {
						updateUI(func() {
							lblStatus.SetText(fmt.Sprintf("Stream disconnected: %v. Reconnecting in 3 seconds...", err))
						})
						select {
						case <-ctx.Done():
							updateUI(func() {
								isStreaming = false
								lblStatus.SetText("Broadcast stopped.")
								btnStream.SetText("Start Broadcast")
							})
							return
						case <-time.After(3 * time.Second):
						}
					} else {
						updateUI(func() {
							isStreaming = false
							lblStatus.SetText("Broadcast completed.")
							btnStream.SetText("Start Broadcast")
						})
						return
					}
				}
			}
		}()
	})

	btnRecordMic.SetOnClick(func() {
		if playbackController.IsPlaying() {
			playbackController.Stop()
			btnPlayPause.SetText("Play")
			btnStop.SetEnabled(false)
		}

		recordWin := wui.NewWindow()
		recordWin.SetTitle("Recording Voice...")
		recordWin.SetInnerWidth(450)
		recordWin.SetInnerHeight(260)
		recordWin.SetResizable(false)
		recordWin.SetHasMaxButton(false)
		recordWin.SetFont(font)
		recordWin.SetInnerPosition(window.X()+250, window.Y()+100)

		lblRecStatus := wui.NewLabel()
		lblRecStatus.SetBounds(20, 15, 410, 25)
		lblRecStatus.SetText("● RECORDING LIVE VOICE")
		recordWin.Add(lblRecStatus)

		lblRecTimer := wui.NewLabel()
		lblRecTimer.SetBounds(20, 45, 410, 25)
		lblRecTimer.SetText("Duration: 0.0s")
		recordWin.Add(lblRecTimer)

		recPaintBox := wui.NewPaintBox()
		recPaintBox.SetBounds(20, 80, 410, 100)
		recordWin.Add(recPaintBox)

		btnStopRec := wui.NewButton()
		btnStopRec.SetBounds(20, 200, 410, 35)
		btnStopRec.SetText("Stop & Save")
		recordWin.Add(btnStopRec)

		var recordedSamples []float64
		var recordedSamplesMutex sync.RWMutex

		recPaintBox.SetOnPaint(func(canvas *wui.Canvas) {
			w, h := recPaintBox.Width(), recPaintBox.Height()
			canvas.FillRect(0, 0, w, h, wui.RGB(8, 12, 18))

			centerY := h / 2
			canvas.Line(0, centerY, w, centerY, wui.RGB(30, 40, 55))

			recordedSamplesMutex.RLock()
			totalSamples := len(recordedSamples)
			visSamples := 2000
			if totalSamples < visSamples {
				visSamples = totalSamples
			}

			if visSamples > 0 {
				samplesSlice := recordedSamples[totalSamples-visSamples:]
				recordedSamplesMutex.RUnlock()

				step := float64(visSamples) / float64(w)
				if step < 1 {
					step = 1
				}

				for i := 0; i < w-1; i++ {
					startIdx := int(float64(i) * step)
					endIdx := int(float64(i+1) * step)
					if endIdx > len(samplesSlice) {
						endIdx = len(samplesSlice)
					}
					if startIdx >= len(samplesSlice) {
						break
					}

					maxVal := 0.0
					for idx := startIdx; idx < endIdx; idx++ {
						val := math.Abs(samplesSlice[idx])
						if val > maxVal {
							maxVal = val
						}
					}

					lineHeight := int(maxVal * float64(centerY-5))
					y1 := centerY - lineHeight
					y2 := centerY + lineHeight

					canvas.Line(i, y1, i, y2, wui.RGB(0, 180, 150))
				}
			} else {
				recordedSamplesMutex.RUnlock()
				canvas.TextOut(w/2-50, h/2-8, "Waiting for audio...", wui.RGB(100, 120, 140))
			}
		})

		recCtx, recCancel := context.WithCancel(context.Background())
		micDev := getDefaultDshowAudioDevice()
		if micDev == "" {
			wui.MessageBoxError("Error", "No audio recording devices (microphone) detected on this system.")
			recCancel()
			return
		}

		cmd := exec.CommandContext(recCtx, "ffmpeg", "-f", "dshow", "-i", "audio="+micDev, "-f", "s16le", "-ac", "1", "-ar", "44100", "-")
		
		stdinPipe, err := cmd.StdinPipe()
		if err != nil {
			wui.MessageBoxError("Error", "Failed to create stdin pipe for recorder: " + err.Error())
			recCancel()
			return
		}

		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			wui.MessageBoxError("Error", "Failed to create stdout pipe for recorder: " + err.Error())
			recCancel()
			return
		}

		err = cmd.Start()
		if err != nil {
			wui.MessageBoxError("Error", "Failed to start live voice recording: " + err.Error())
			recCancel()
			return
		}

		go func() {
			reader := bufio.NewReader(stdoutPipe)
			localBuf := make([]float64, 0, 1024)
			var sampleBuf [2]byte

			for {
				_, err := io.ReadFull(reader, sampleBuf[:])
				if err != nil {
					break
				}
				val := int16(binary.LittleEndian.Uint16(sampleBuf[:]))
				sample := float64(val) / 32767.0
				localBuf = append(localBuf, sample)

				if len(localBuf) >= 1024 {
					recordedSamplesMutex.Lock()
					recordedSamples = append(recordedSamples, localBuf...)
					recordedSamplesMutex.Unlock()
					localBuf = localBuf[:0]
				}
			}

			if len(localBuf) > 0 {
				recordedSamplesMutex.Lock()
				recordedSamples = append(recordedSamples, localBuf...)
				recordedSamplesMutex.Unlock()
			}
		}()

		startTime := time.Now()
		ticker := time.NewTicker(40 * time.Millisecond)
		pulseState := true

		go func() {
			for {
				select {
				case <-recCtx.Done():
					ticker.Stop()
					return
				case <-ticker.C:
					elapsed := time.Since(startTime).Seconds()
					updateUI(func() {
						lblRecTimer.SetText(fmt.Sprintf("Duration: %.1fs", elapsed))
						if int(elapsed*10)%10 == 0 {
							if pulseState {
								lblRecStatus.SetText("  RECORDING LIVE VOICE")
							} else {
								lblRecStatus.SetText("● RECORDING LIVE VOICE")
							}
							pulseState = !pulseState
						}
						recPaintBox.Paint()
					})
				}
			}
		}()

		finalizeRecording := func() {
			if stdinPipe != nil {
				_, _ = stdinPipe.Write([]byte("q\n"))
				_ = stdinPipe.Close()
			}

			done := make(chan error, 1)
			go func() {
				done <- cmd.Wait()
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				recCancel()
			}

			ticker.Stop()
			time.Sleep(100 * time.Millisecond)

			recordedSamplesMutex.RLock()
			samplesToSave := make([]float64, len(recordedSamples))
			copy(samplesToSave, recordedSamples)
			recordedSamplesMutex.RUnlock()

			saveErr := writeWavFile("mic_record.wav", samplesToSave, sampleRate)

			updateUI(func() {
				recordWin.Close()
				if saveErr == nil && len(samplesToSave) > 0 {
					txtExternalAudio.SetText("mic_record.wav")
					lblStatus.SetText("Successfully recorded microphone! Loaded into External Audio.")
				} else {
					if saveErr != nil {
						lblStatus.SetText("Recording ended, but WAV generation failed: " + saveErr.Error())
					} else {
						lblStatus.SetText("Recording ended, but no audio samples were captured.")
					}
				}
			})
		}

		btnStopRec.SetOnClick(func() {
			finalizeRecording()
		})

		go func() {
			select {
			case <-recCtx.Done():
				return
			case <-time.After(120 * time.Second):
				finalizeRecording()
			}
		}()

		recordWin.ShowModal()
	})

	// Background ticker for the real-time split-screen visualizer (secPaintBox)
	go func() {
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			updateUI(func() {
				secPaintBox.Paint()
			})
		}
	}()

	window.Show()
}

// --- Driver Core Entry Point ---

func main() {
	cliMode := flag.Bool("cli", false, "CLI mode (not implemented)")
	flag.Parse()

	if *cliMode {
		fmt.Println("CLI mode not configured - please use GUI mode")
		os.Exit(1)
	}

	runGUI()
}