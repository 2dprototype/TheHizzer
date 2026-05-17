package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
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
}

// --- Audio Processing Parameters ---

type AudioProcessor struct {
	// UVB-76 specific parameters
	BuzzerEnabled     bool
	BuzzerFrequency   float64 // 4625 Hz typical UVB-76 carrier
	BuzzerPulseRate   float64 // Pulses per second
	BuzzerModDepth    float64 // Modulation depth

	// Filter parameters
	LowPassEnabled    bool
	LowPassCutoff     float64 // Hz
	HighPassEnabled   bool
	HighPassCutoff    float64 // Hz
	BandPassEnabled   bool
	BandPassCenter    float64 // Hz
	BandPassQ         float64 // Quality factor

	// Distortion effects
	DistortionEnabled bool
	DistortionAmount  float64 // 0-1
	BitCrushEnabled   bool
	BitCrushDepth     int     // Bit reduction
	SampleRateReduceEnabled bool
	SampleRateReduce  int     // Sample rate reduction

	// Modulation effects
	RingModEnabled        bool
	RingModFreq           float64 // Hz
	AmplitudeModEnabled   bool
	AmplitudeModFreq      float64 // Hz
	AmplitudeModDepth     float64 // 0-1
	PhaseModEnabled       bool
	PhaseModFreq          float64 // Hz
	PhaseModDepth         float64 // radians

	// Time-based effects
	ReverbEnabled     bool
	ReverbAmount      float64 // 0-1
	ReverbDecay       float64 // seconds
	DelayEnabled      bool
	DelayTime         float64 // seconds
	DelayFeedback     float64 // 0-1
	DelayAmount       float64 // 0-1

	// Noise generation
	NoiseFloorEnabled bool
	NoiseFloor        float64 // -60 to 0 dB
	PulseInterference bool    // Simulated pulse interference
	StaticCrackleEnabled bool
	StaticCrackle     float64 // Static amount 0-1

	// EQ bands
	EqEnabled         bool
	EqBassGain        float64 // dB
	EqMidGain         float64 // dB
	EqTrebleGain      float64 // dB

	// Compression
	CompressorEnabled   bool
	CompressorThreshold float64 // -60 to 0 dB
	CompressorRatio     float64 // 1:1 to 20:1
	CompressorAttack    float64 // ms
	CompressorRelease   float64 // ms

	// Special effects
	WowFlutterEnabled  bool   // Tape wow/flutter simulation
	RadioFading        bool   // Simulate HF radio fading
	FilterSweepEnabled bool   // Sweeping filter effect

	// New Special Filters
	RadioNoiseEnabled     bool
	WalkieTalkieEnabled   bool
	DistortedAudioEnabled bool
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
	zoomLevel      = 1.0
	zoomOffset     = 0.0
	isStreaming    = false
	streamCancel   context.CancelFunc
	globalPaintBox *wui.PaintBox

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

// --- UVB-76 Style Audio Processing Functions ---

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
	// - Continuous UVB-76 Buzzer Carrier
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

func generateAudioBuffers(morseCode string, externalAudioPath string) []float64 {
	freq := 800.0
	dotDuration := 0.1

	dotSamples := int(float64(sampleRate) * dotDuration)
	dashSamples := dotSamples * 3
	silenceSamples := dotSamples

	var morseSamples []float64

	generateTone := func(numSamples int) []float64 {
		res := make([]float64, numSamples)
		fadeSamples := int(float64(sampleRate) * 0.01)
		for i := 0; i < numSamples; i++ {
			t := float64(i) / float64(sampleRate)
			val := math.Sin(2 * math.Pi * freq * t)

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

	// Load external audio if provided
	var externalSamples []float64
	if externalAudioPath != "" {
		if ext, err := loadExternalAudio(externalAudioPath); err == nil {
			externalSamples = ext
		}
	}

	// Mix both signals
	var mixed []float64
	if len(morseSamples) > 0 && len(externalSamples) > 0 {
		maxLen := len(morseSamples)
		if len(externalSamples) > maxLen {
			maxLen = len(externalSamples)
		}
		mixed = make([]float64, maxLen)
		for i := 0; i < maxLen; i++ {
			morseVal := morseSamples[i%len(morseSamples)]
			externalVal := externalSamples[i%len(externalSamples)]
			mixed[i] = morseVal + externalVal
		}
	} else if len(morseSamples) > 0 {
		mixed = morseSamples
	} else if len(externalSamples) > 0 {
		mixed = externalSamples
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

// --- Media Pipeline Control Engine ---

func executeStreamPipeline(ctx context.Context, cfg Config) error {
	var args []string
	args = append(args, "-re", "-loop", "1", "-i", cfg.BackgroundImage)
	args = append(args, "-f", "s16le", "-ar", "44100", "-ac", "1", "-i", "pipe:0")
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast",
		"-b:v", cfg.VideoBitrate, "-maxrate", cfg.VideoBitrate, "-bufsize", "6000k",
		"-pix_fmt", "yuv420p", "-g", "60", "-r", fmt.Sprintf("%d", cfg.FPS),
		"-c:a", "aac", "-b:a", cfg.AudioBitrate, "-ar", "44100",
		"-vf", fmt.Sprintf("fps=%d,scale=1920:1080,format=yuv420p,drawtext=text='%%{pts\\:localtime}':x=10:y=10:fontsize=24:fontcolor=white", cfg.FPS),
		"-af", "aresample=44100",
	)

	if cfg.Duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", cfg.Duration))
	}

	if cfg.OutputFile != "" {
		args = append(args, cfg.OutputFile)
	} else {
		args = append(args, "-f", "flv", cfg.RTMPURL)
	}
	args = append(args, "-y")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		defer stdin.Close()
		
		pos := 0
		chunkSize := 1024
		buf := make([]byte, chunkSize*2)
		
		currentVersion := -1
		var activeSamples []float64
		
		for {
			select {
			case <-ctx.Done():
				return
			default:
				audioMutex.RLock()
				globalSamples := audioSamples
				version := audioVersion
				audioMutex.RUnlock()
				
				if version != currentVersion || len(activeSamples) == 0 {
					currentVersion = version
					if len(globalSamples) > 0 {
						activeSamples = make([]float64, len(globalSamples))
						copy(activeSamples, globalSamples)
					} else {
						activeSamples = nil
					}
					pos = 0
				}
				
				if len(activeSamples) == 0 {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				
				for i := 0; i < chunkSize; i++ {
					sampleIdx := (pos + i) % len(activeSamples)
					val := int16(activeSamples[sampleIdx] * 32767.0)
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(val))
				}
				
				pos = (pos + chunkSize) % len(activeSamples)
				
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
	for i := range samples {
		if s.pos >= len(s.samples) {
			return i, true
		}
		val := s.samples[s.pos]
		samples[i][0] = val
		samples[i][1] = val
		s.pos++
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
	settingsWin.SetTitle("Audio Processing Settings - UVB-76 Synth Dashboard")

	font, _ := wui.NewFont(wui.FontDesc{Name: "Segoe UI", Height: -12})
	settingsWin.SetFont(font)

	// Header Panel (Y: 10 to 45)
	lblPresets := wui.NewLabel()
	lblPresets.SetBounds(20, 15, 60, 20)
	lblPresets.SetText("Presets:")
	settingsWin.Add(lblPresets)

	btnUVB76 := wui.NewButton()
	btnUVB76.SetBounds(90, 10, 100, 28)
	btnUVB76.SetText("UVB-76")
	settingsWin.Add(btnUVB76)

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
	btnUVB76.SetOnClick(func() {
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
	})

	sepFooter := wui.NewLabel()
	sepFooter.SetBounds(20, 445, 910, 2)
	sepFooter.SetText("──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────")
	settingsWin.Add(sepFooter)

	btnClose := wui.NewButton()
	btnClose.SetBounds(415, 452, 120, 30)
	btnClose.SetText("Apply & Close")
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

		settingsWin.Close()
	})

	settingsWin.ShowModal()
}

func runGUI() {
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
	window.SetTitle("UVB-76 Morse Code Broadcast Engine - The Buzzer")

	// Input Labels and Edit Fields
	lblMsg := wui.NewLabel()
	lblMsg.SetBounds(20, 15, 110, 20)
	lblMsg.SetText("Morse Message:")
	window.Add(lblMsg)

	txtMsg := wui.NewEditLine()
	txtMsg.SetBounds(140, 13, 330, 24)
	txtMsg.SetText("CQ CQ DE UVB-76")
	window.Add(txtMsg)

	lblBg := wui.NewLabel()
	lblBg.SetBounds(20, 45, 110, 20)
	lblBg.SetText("Background Image:")
	window.Add(lblBg)

	txtBg := wui.NewEditLine()
	txtBg.SetBounds(140, 43, 330, 24)
	txtBg.SetText("background.jpg")
	window.Add(txtBg)

	lblRtmp := wui.NewLabel()
	lblRtmp.SetBounds(500, 15, 100, 20)
	lblRtmp.SetText("RTMP Endpoint:")
	window.Add(lblRtmp)

	txtRtmp := wui.NewEditLine()
	txtRtmp.SetBounds(610, 13, 320, 24)
	txtRtmp.SetText("rtmp://a.rtmp.youtube.com/live2/YOUR_STREAM_KEY")
	window.Add(txtRtmp)

	lblOut := wui.NewLabel()
	lblOut.SetBounds(500, 45, 100, 20)
	lblOut.SetText("Local Output:")
	window.Add(lblOut)

	txtOut := wui.NewEditLine()
	txtOut.SetBounds(610, 43, 320, 24)
	txtOut.SetText("")
	window.Add(txtOut)

	lblExternalAudio := wui.NewLabel()
	lblExternalAudio.SetBounds(20, 75, 110, 20)
	lblExternalAudio.SetText("External Audio:")
	window.Add(lblExternalAudio)

	txtExternalAudio := wui.NewEditLine()
	txtExternalAudio.SetBounds(140, 73, 260, 24)
	txtExternalAudio.SetText("")
	window.Add(txtExternalAudio)

	btnBrowseAudio := wui.NewButton()
	btnBrowseAudio.SetBounds(405, 72, 65, 26)
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

	lblDur := wui.NewLabel()
	lblDur.SetBounds(500, 75, 100, 20)
	lblDur.SetText("Duration (Secs):")
	window.Add(lblDur)

	txtDur := wui.NewEditLine()
	txtDur.SetBounds(610, 73, 80, 24)
	txtDur.SetText("0")
	window.Add(txtDur)

	chkLiveUpdate := wui.NewCheckBox()
	chkLiveUpdate.SetBounds(710, 74, 150, 22)
	chkLiveUpdate.SetText("Live Update Stream")
	chkLiveUpdate.SetChecked(true)
	window.Add(chkLiveUpdate)

	btnGenerate := wui.NewButton()
	btnGenerate.SetBounds(500, 103, 180, 26)
	btnGenerate.SetText("Generate with Effects")
	window.Add(btnGenerate)

	btnAudioSettings := wui.NewButton()
	btnAudioSettings.SetBounds(695, 103, 160, 26)
	btnAudioSettings.SetText("🎛️ Audio Settings")
	window.Add(btnAudioSettings)

	paintBox := wui.NewPaintBox()
	paintBox.SetBounds(20, 135, 910, 220)
	window.Add(paintBox)
	globalPaintBox = paintBox

	btnPlayPause := wui.NewButton()
	btnPlayPause.SetBounds(20, 365, 150, 40)
	btnPlayPause.SetText("▶ Play")
	window.Add(btnPlayPause)
	
	btnStop := wui.NewButton()
	btnStop.SetBounds(180, 365, 100, 40)
	btnStop.SetText("⏹ Stop")
	btnStop.SetEnabled(false)
	window.Add(btnStop)

	btnZoomIn := wui.NewButton()
	btnZoomIn.SetBounds(295, 365, 90, 40)
	btnZoomIn.SetText("Zoom In")
	window.Add(btnZoomIn)

	btnZoomOut := wui.NewButton()
	btnZoomOut.SetBounds(395, 365, 90, 40)
	btnZoomOut.SetText("Zoom Out")
	window.Add(btnZoomOut)

	btnResetZoom := wui.NewButton()
	btnResetZoom.SetBounds(495, 365, 90, 40)
	btnResetZoom.SetText("Reset")
	window.Add(btnResetZoom)

	btnStream := wui.NewButton()
	btnStream.SetBounds(600, 365, 330, 40)
	btnStream.SetText("🚀 Launch Transmit Chain")
	btnStream.SetFont(font)
	window.Add(btnStream)

	lblStatus := wui.NewLabel()
	lblStatus.SetBounds(20, 415, 910, 50)
	lblStatus.SetText("System Ready. Configure audio effects and generate Morse payload.")
	window.Add(lblStatus)

	updateUI := func(updateFunc func()) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			updateFunc()
		}()
	}

	audioSamples = generateAudioBuffers(textToMorse(txtMsg.Text()), strings.TrimSpace(txtExternalAudio.Text()))
	waveformData = computeWaveform(audioSamples, paintBox.Width())

	btnGenerate.SetOnClick(func() {
		btnGenerate.SetEnabled(false)
		lblStatus.SetText("Generating audio with effects... Please wait.")
		
		go func() {
			morseStr := textToMorse(txtMsg.Text())
			newSamples := generateAudioBuffers(morseStr, strings.TrimSpace(txtExternalAudio.Text()))
			
			updateUI(func() {
				audioMutex.Lock()
				audioSamples = newSamples
				if chkLiveUpdate.Checked() {
					audioVersion++
				}
				audioMutex.Unlock()
				
				zoomOffset = 0
				waveformData = computeWaveform(audioSamples, paintBox.Width())
				paintBox.Paint()
				btnGenerate.SetEnabled(true)
				lblStatus.SetText(fmt.Sprintf("Audio generated with effects. Total duration: %.2f seconds", float64(len(audioSamples))/float64(sampleRate)))
			})
		}()
	})

	btnAudioSettings.SetOnClick(func() {
		showAudioSettingsDialog(window)
		btnGenerate.SetEnabled(false)
		lblStatus.SetText("Applying new audio settings...")
		
		go func() {
			morseStr := textToMorse(txtMsg.Text())
			newSamples := generateAudioBuffers(morseStr, strings.TrimSpace(txtExternalAudio.Text()))
			
			updateUI(func() {
				audioMutex.Lock()
				audioSamples = newSamples
				if chkLiveUpdate.Checked() {
					audioVersion++
				}
				audioMutex.Unlock()
				
				waveformData = computeWaveform(audioSamples, paintBox.Width())
				paintBox.Paint()
				btnGenerate.SetEnabled(true)
				lblStatus.SetText("Audio settings applied. Regenerated with new effects.")
			})
		}()
	})

	paintBox.SetOnPaint(func(canvas *wui.Canvas) {
		w, h := paintBox.Width(), paintBox.Height()
		centerY := h / 2

		canvas.FillRect(0, 0, w, h, wui.RGB(8, 12, 18))

		audioMutex.RLock()
		currentWaveform := waveformData
		audioMutex.RUnlock()
		
		if len(currentWaveform) == 0 {
			return
		}

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

		canvas.Line(0, centerY, w, centerY, wui.RGB(60, 60, 80))

		startBarX := int(zoomOffset)
		endBarX := int(float64(w)*zoomLevel + zoomOffset)
		if startBarX >= 0 && startBarX < w {
			canvas.Line(startBarX, 0, startBarX, h, wui.RGB(255, 100, 50))
		}
		if endBarX >= 0 && endBarX < w {
			canvas.Line(endBarX, 0, endBarX, h, wui.RGB(255, 100, 50))
		}
	})

	btnZoomIn.SetOnClick(func() {
		zoomLevel *= 1.3
		if zoomLevel > 20 {
			zoomLevel = 20
		}
		waveformData = computeWaveform(audioSamples, paintBox.Width())
		paintBox.Paint()
	})

	btnZoomOut.SetOnClick(func() {
		zoomLevel /= 1.3
		if zoomLevel < 1.0 {
			zoomLevel = 1.0
		}
		waveformData = computeWaveform(audioSamples, paintBox.Width())
		paintBox.Paint()
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
			btnPlayPause.SetText("⏸ Pause")
			
			playbackController.Play(samples, func() {
				updateUI(func() {
					btnPlayPause.SetText("▶ Play")
					btnStop.SetEnabled(false)
					lblStatus.SetText("Playback completed.")
				})
			})
		} else {
			if playbackController.IsPaused() {
				playbackController.Resume()
				btnPlayPause.SetText("⏸ Pause")
				lblStatus.SetText("Playback resumed.")
			} else {
				playbackController.Pause()
				btnPlayPause.SetText("▶ Resume")
				lblStatus.SetText("Playback paused.")
			}
		}
	})
	
	btnStop.SetOnClick(func() {
		if playbackController.IsPlaying() {
			playbackController.Stop()
			btnPlayPause.SetText("▶ Play")
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
		if _, err := os.Stat(bgPath); os.IsNotExist(err) {
			lblStatus.SetText("Warning: Background not found. Creating generic black layout...")
			if err := createDefaultBackground(bgPath); err != nil {
				wui.MessageBoxError("Error", "Could not create background")
				return
			}
		}

		var durSec int
		fmt.Sscanf(txtDur.Text(), "%d", &durSec)

		cfg := Config{
			BackgroundImage: bgPath,
			RTMPURL:         strings.TrimSpace(txtRtmp.Text()),
			MorseCode:       txtMsg.Text(),
			FPS:             30,
			VideoBitrate:    "3000k",
			AudioBitrate:    "128k",
			Duration:        durSec,
			OutputFile:      strings.TrimSpace(txtOut.Text()),
		}

		if cfg.RTMPURL == "" && cfg.OutputFile == "" {
			wui.MessageBoxError("Error", "Please specify RTMP URL or output file")
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		streamCancel = cancel
		isStreaming = true

		btnStream.SetText("🛑 Stop Broadcasting")
		lblStatus.SetText("Broadcasting with UVB-76 effects...")

		go func() {
			for {
				select {
				case <-ctx.Done():
					updateUI(func() {
						isStreaming = false
						lblStatus.SetText("Broadcast stopped.")
						btnStream.SetText("🚀 Launch Transmit Chain")
					})
					return
				default:
					err := executeStreamPipeline(ctx, cfg)
					
					if ctx.Err() != nil {
						updateUI(func() {
							isStreaming = false
							lblStatus.SetText("Broadcast stopped.")
							btnStream.SetText("🚀 Launch Transmit Chain")
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
								btnStream.SetText("🚀 Launch Transmit Chain")
							})
							return
						case <-time.After(3 * time.Second):
						}
					} else {
						updateUI(func() {
							isStreaming = false
							lblStatus.SetText("Broadcast completed.")
							btnStream.SetText("🚀 Launch Transmit Chain")
						})
						return
					}
				}
			}
		}()
	})

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