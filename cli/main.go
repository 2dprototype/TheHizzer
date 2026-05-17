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
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// --- Styles ---

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")).
			Background(lipgloss.Color("#1A1A2E")).
			Padding(1, 2).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FF6B6B"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ECDC4")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4444")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#45B7D1")).
			Italic(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFE66D")).
			Bold(true)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	boxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4ECDC4")).
		Padding(1, 2).
		MarginTop(1)

	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2A2A3E")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ECDC4"))
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

type AudioProcessor struct {
	BuzzerEnabled           bool    `json:"buzzer_enabled"`
	BuzzerFrequency         float64 `json:"buzzer_frequency"`
	BuzzerPulseRate         float64 `json:"buzzer_pulse_rate"`
	BuzzerModDepth          float64 `json:"buzzer_mod_depth"`
	LowPassEnabled          bool    `json:"low_pass_enabled"`
	LowPassCutoff           float64 `json:"low_pass_cutoff"`
	HighPassEnabled         bool    `json:"high_pass_enabled"`
	HighPassCutoff          float64 `json:"high_pass_cutoff"`
	DistortionEnabled       bool    `json:"distortion_enabled"`
	DistortionAmount        float64 `json:"distortion_amount"`
	BitCrushEnabled         bool    `json:"bit_crush_enabled"`
	BitCrushDepth           int     `json:"bit_crush_depth"`
	SampleRateReduceEnabled bool    `json:"sample_rate_reduce_enabled"`
	SampleRateReduce        int     `json:"sample_rate_reduce"`
	RingModEnabled          bool    `json:"ring_mod_enabled"`
	RingModFreq             float64 `json:"ring_mod_freq"`
	AmplitudeModEnabled     bool    `json:"amplitude_mod_enabled"`
	AmplitudeModFreq        float64 `json:"amplitude_mod_freq"`
	AmplitudeModDepth       float64 `json:"amplitude_mod_depth"`
	ReverbEnabled           bool    `json:"reverb_enabled"`
	ReverbAmount            float64 `json:"reverb_amount"`
	ReverbDecay             float64 `json:"reverb_decay"`
	DelayEnabled            bool    `json:"delay_enabled"`
	DelayTime               float64 `json:"delay_time"`
	DelayFeedback           float64 `json:"delay_feedback"`
	DelayAmount             float64 `json:"delay_amount"`
	NoiseFloorEnabled       bool    `json:"noise_floor_enabled"`
	NoiseFloor              float64 `json:"noise_floor"`
	PulseInterference       bool    `json:"pulse_interference"`
	StaticCrackleEnabled    bool    `json:"static_crackle_enabled"`
	StaticCrackle           float64 `json:"static_crackle"`
	EqEnabled               bool    `json:"eq_enabled"`
	EqBassGain              float64 `json:"eq_bass_gain"`
	EqMidGain               float64 `json:"eq_mid_gain"`
	EqTrebleGain            float64 `json:"eq_treble_gain"`
	CompressorEnabled       bool    `json:"compressor_enabled"`
	CompressorThreshold     float64 `json:"compressor_threshold"`
	CompressorRatio         float64 `json:"compressor_ratio"`
	WowFlutterEnabled       bool    `json:"wow_flutter_enabled"`
	RadioFading             bool    `json:"radio_fading"`
	FilterSweepEnabled      bool    `json:"filter_sweep_enabled"`
	RadioNoiseEnabled       bool    `json:"radio_noise_enabled"`
	WalkieTalkieEnabled     bool    `json:"walkie_talkie_enabled"`
	DistortedAudioEnabled   bool    `json:"distorted_audio_enabled"`
	BeepVolume              float64 `json:"beep_volume"`
	BeepFrequency           float64 `json:"beep_frequency"`
	BeepDotDuration         float64 `json:"beep_dot_duration"`
	ExternalAudioVolume     float64 `json:"external_audio_volume"`
}

// --- Global Variables ---

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

	sampleRate     = 44100
	audioSamples   []float64
	audioMutex     sync.RWMutex
	audioProc      = &AudioProcessor{
		BuzzerEnabled:           true,
		BuzzerFrequency:         4625.0,
		BuzzerPulseRate:         1.0,
		BuzzerModDepth:          0.8,
		LowPassEnabled:          true,
		LowPassCutoff:           3400.0,
		HighPassEnabled:         true,
		HighPassCutoff:          300.0,
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

// --- Model for Bubble Tea TUI ---

type model struct {
	spinner      spinner.Model
	progress     progress.Model
	status       string
	errorMsg     string
	ready        bool
	audioGenDone bool
	audioLen     float64
	outputPath   string
	streamActive bool
	ctx          context.Context
	cancel       context.CancelFunc
	cfg          CLIConfig
}

type CLIConfig struct {
	Message         string
	BackgroundImage string
	RTMPURL         string
	OutputFile      string
	ExternalAudio   string
	Duration        int
	Preset          string
}

type generationCompleteMsg struct {
	duration float64
}

type generationErrorMsg struct {
	err error
}

type streamStartedMsg struct{}

type streamStoppedMsg struct{}

type tickMsg struct{}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.generateAudio,
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.streamActive && m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}

	case generationCompleteMsg:
		m.audioGenDone = true
		m.audioLen = msg.duration
		m.status = "Audio generation complete!"
		
		if m.cfg.RTMPURL != "" || m.cfg.OutputFile != "" {
			m.status = "Starting broadcast..."
			return m, m.startStream
		}
		return m, tea.Quit

	case generationErrorMsg:
		m.errorMsg = msg.err.Error()
		m.status = "Error generating audio"
		return m, tea.Quit

	case streamStartedMsg:
		m.streamActive = true
		m.status = "Broadcasting live..."
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })

	case streamStoppedMsg:
		m.streamActive = false
		m.status = "Broadcast completed"
		return m, tea.Quit

	case tickMsg:
		if m.streamActive {
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	var sb strings.Builder

	// Header
	sb.WriteString(titleStyle.Render("The Hizzer"))
	sb.WriteString("\n\n")

	// Config Display
	sb.WriteString(boxStyle.Render(
		labelStyle.Render("Configuration") + "\n" +
			"  " + infoStyle.Render("Message:") + " " + valueStyle.Render(m.cfg.Message) + "\n" +
			"  " + infoStyle.Render("Background:") + " " + valueStyle.Render(m.cfg.BackgroundImage) + "\n" +
			"  " + infoStyle.Render("External Audio:") + " " + valueStyle.Render(func() string {
			if m.cfg.ExternalAudio != "" {
				return m.cfg.ExternalAudio
			}
			return "(none)"
		}()) + "\n" +
			"  " + infoStyle.Render("Preset:") + " " + valueStyle.Render(m.cfg.Preset) + "\n" +
			"  " + infoStyle.Render("RTMP:") + " " + valueStyle.Render(func() string {
			if m.cfg.RTMPURL != "" {
				return m.cfg.RTMPURL
			}
			return "(none)"
		}()) + "\n" +
			"  " + infoStyle.Render("Output:") + " " + valueStyle.Render(func() string {
			if m.cfg.OutputFile != "" {
				return m.cfg.OutputFile
			}
			return "(none)"
		}()) + "\n" +
			"  " + infoStyle.Render("Duration:") + " " + valueStyle.Render(func() string {
			if m.cfg.Duration > 0 {
				return fmt.Sprintf("%d seconds", m.cfg.Duration)
			}
			return "infinite"
		}()),
	))
	sb.WriteString("\n\n")

	// Audio Settings Summary
	sb.WriteString(boxStyle.Render(
		labelStyle.Render("Audio Processing Chain") + "\n" +
			"  " + infoStyle.Render("Buzzer:") + " " + valueStyle.Render(fmt.Sprintf("%.0f Hz, %.1f Hz pulse", audioProc.BuzzerFrequency, audioProc.BuzzerPulseRate)) + "\n" +
			"  " + infoStyle.Render("Filters:") + " " + valueStyle.Render(fmt.Sprintf("LP %.0f Hz / HP %.0f Hz", audioProc.LowPassCutoff, audioProc.HighPassCutoff)) + "\n" +
			"  " + infoStyle.Render("Distortion:") + " " + valueStyle.Render(fmt.Sprintf("%.0f%%", audioProc.DistortionAmount*100)) + "\n" +
			"  " + infoStyle.Render("Bit Crush:") + " " + valueStyle.Render(fmt.Sprintf("%d-bit", audioProc.BitCrushDepth)) + "\n" +
			"  " + infoStyle.Render("Reverb/Delay:") + " " + valueStyle.Render(fmt.Sprintf("%.0f%% / %.0f%%", audioProc.ReverbAmount*100, audioProc.DelayAmount*100)) + "\n" +
			"  " + infoStyle.Render("Noise Floor:") + " " + valueStyle.Render(fmt.Sprintf("%.0f dB", audioProc.NoiseFloor)),
	))
	sb.WriteString("\n\n")

	// Status
	if m.audioGenDone && m.audioLen > 0 {
		sb.WriteString(successStyle.Render(fmt.Sprintf("Audio generated: %.2f seconds", m.audioLen)))
		sb.WriteString("\n")
	}

	if m.streamActive {
		sb.WriteString(progressStyle.Render("LIVE BROADCASTING... Press 'q' to stop"))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(m.spinner.View())
	sb.WriteString(" ")
	sb.WriteString(infoStyle.Render(m.status))
	sb.WriteString("\n\n")

	if m.errorMsg != "" {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %s", m.errorMsg)))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(infoStyle.Render("Press 'q' or Ctrl+C to quit"))
	sb.WriteString("\n")

	return sb.String()
}

func (m *model) generateAudio() tea.Msg {
	morseStr := textToMorse(m.cfg.Message)
	samples := generateAudioBuffers(morseStr, m.cfg.ExternalAudio)
	
	audioMutex.Lock()
	audioSamples = samples
	audioMutex.Unlock()
	
	duration := float64(len(samples)) / float64(sampleRate)
	return generationCompleteMsg{duration: duration}
}

func (m *model) startStream() tea.Msg {
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel

	go func() {
		cfg := Config{
			BackgroundImage: m.cfg.BackgroundImage,
			RTMPURL:         m.cfg.RTMPURL,
			MorseCode:       m.cfg.Message,
			FPS:             30,
			VideoBitrate:    "3000k",
			AudioBitrate:    "128k",
			Duration:        m.cfg.Duration,
			OutputFile:      m.cfg.OutputFile,
		}

		if cfg.RTMPURL == "" && cfg.OutputFile == "" {
			return
		}

		err := executeStreamPipeline(ctx, cfg)
		if err != nil && ctx.Err() == nil {
			fmt.Printf("\n%s\n", errorStyle.Render(fmt.Sprintf("Stream error: %v", err)))
		}
	}()

	return streamStartedMsg{}
}

// --- Audio Processing Functions (same as original) ---

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
	freq := audioProc.BeepFrequency
	if freq <= 0 {
		freq = 800.0
	}
	dotDuration := audioProc.BeepDotDuration
	if dotDuration <= 0 {
		dotDuration = 0.1
	}
	beepVol := audioProc.BeepVolume
	externalVol := audioProc.ExternalAudioVolume

	dotSamples := int(float64(sampleRate) * dotDuration)
	dashSamples := dotSamples * 3
	silenceSamples := dotSamples

	var morseSamples []float64

	generateTone := func(numSamples int) []float64 {
		res := make([]float64, numSamples)
		fadeSamples := int(float64(sampleRate) * 0.01)
		for i := 0; i < numSamples; i++ {
			t := float64(i) / float64(sampleRate)
			val := math.Sin(2*math.Pi*freq*t) * beepVol

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

	var externalSamples []float64
	if externalAudioPath != "" {
		if ext, err := loadExternalAudio(externalAudioPath); err == nil {
			externalSamples = ext
			for i := range externalSamples {
				externalSamples[i] *= externalVol
			}
		}
	}

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

	processed := processAudioEffects(mixed, audioProc)
	return processed
}

func processAudioEffects(samples []float64, proc *AudioProcessor) []float64 {
	if len(samples) == 0 {
		return samples
	}

	processed := make([]float64, len(samples))
	copy(processed, samples)

	// Special filters
	if proc.RadioNoiseEnabled {
		processed = applyRadioNoise(processed)
	}
	if proc.WalkieTalkieEnabled {
		processed = applyWalkieTalkie(processed)
	}
	if proc.DistortedAudioEnabled {
		processed = applyDistortedAudio(processed)
	}

	// Buzzer and noise
	if proc.BuzzerEnabled {
		processed = addBuzzerCarrier(processed, proc.BuzzerFrequency, proc.BuzzerPulseRate, proc.BuzzerModDepth)
	}
	if proc.NoiseFloorEnabled && proc.NoiseFloor < 0 {
		processed = addNoiseFloor(processed, proc.NoiseFloor)
	}
	if proc.PulseInterference {
		processed = addPulseInterference(processed, 0.15)
	}
	if proc.StaticCrackleEnabled && proc.StaticCrackle > 0 {
		processed = applyStaticCrackle(processed, proc.StaticCrackle)
	}

	// Filters
	if proc.LowPassEnabled && proc.LowPassCutoff < float64(sampleRate)/2 {
		processed = applyLowPassFilter(processed, proc.LowPassCutoff)
	}
	if proc.HighPassEnabled && proc.HighPassCutoff > 0 {
		processed = applyHighPassFilter(processed, proc.HighPassCutoff)
	}
	if proc.FilterSweepEnabled {
		processed = applyFilterSweep(processed, proc.LowPassCutoff, 4000, 3.0)
	}

	// Distortion and crushing
	if proc.DistortionEnabled && proc.DistortionAmount > 0 {
		processed = applyDistortion(processed, proc.DistortionAmount)
	}
	if proc.BitCrushEnabled && proc.BitCrushDepth < 16 {
		processed = applyBitCrushing(processed, proc.BitCrushDepth)
	}
	if proc.SampleRateReduceEnabled && proc.SampleRateReduce < sampleRate {
		processed = applySampleRateReduction(processed, proc.SampleRateReduce)
	}

	// EQ
	if proc.EqEnabled {
		processed = applyEq(processed, proc.EqBassGain, proc.EqMidGain, proc.EqTrebleGain)
	}

	// Modulation
	if proc.RingModEnabled && proc.RingModFreq > 0 {
		processed = applyRingModulation(processed, proc.RingModFreq)
	}
	if proc.AmplitudeModEnabled && proc.AmplitudeModFreq > 0 && proc.AmplitudeModDepth > 0 {
		processed = applyAmplitudeModulation(processed, proc.AmplitudeModFreq, proc.AmplitudeModDepth)
	}

	// Time effects
	if proc.DelayEnabled && proc.DelayAmount > 0 {
		processed = applyDelay(processed, proc.DelayTime, proc.DelayFeedback, proc.DelayAmount)
	}
	if proc.ReverbEnabled && proc.ReverbAmount > 0 {
		processed = applyReverb(processed, proc.ReverbAmount, proc.ReverbDecay)
	}
	if proc.WowFlutterEnabled {
		processed = applyWowFlutter(processed)
	}
	if proc.RadioFading {
		processed = applyRadioFading(processed)
	}
	if proc.CompressorEnabled {
		processed = applyCompressor(processed, proc.CompressorThreshold, proc.CompressorRatio, 5.0, 50.0)
	}

	// Normalization
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

// Filter and effect implementations (same as original, kept for brevity)
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

func applyDistortion(samples []float64, amount float64) []float64 {
	distorted := make([]float64, len(samples))
	for i, sample := range samples {
		distorted[i] = math.Tanh(sample * (1 + amount*5))
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

func applyReverb(samples []float64, amount float64, decaySec float64) []float64 {
	if amount <= 0 {
		return samples
	}
	delaySamples := int(float64(sampleRate) * 0.05)
	decaySamples := int(float64(sampleRate) * decaySec)
	reverbed := make([]float64, len(samples))
	copy(reverbed, samples)
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

func applyEq(samples []float64, bassGain, midGain, trebleGain float64) []float64 {
	if bassGain == 0 && midGain == 0 && trebleGain == 0 {
		return samples
	}
	bassGainLinear := math.Pow(10, bassGain/20)
	midGainLinear := math.Pow(10, midGain/20)
	trebleGainLinear := math.Pow(10, trebleGain/20)
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
		var gainReduction float64
		if absSample > thresholdLinear {
			excess := absSample / thresholdLinear
			compressedLevel := thresholdLinear * math.Pow(excess, 1/ratio)
			gainReduction = compressedLevel / absSample
		} else {
			gainReduction = 1.0
		}
		if gainReduction < envelope {
			envelope = envelope - (envelope-gainReduction)/float64(attackSamples)
		} else {
			envelope = envelope + (gainReduction-envelope)/float64(releaseSamples)
		}
		compressed[i] = sample * envelope
	}
	return compressed
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
	pulseInterval := int(float64(sampleRate) * 0.5)
	pulseDuration := int(float64(sampleRate) * 0.01)
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
			pop := (rand.Float64()*2 - 1) * 0.5 * amount
			crackly[i] = sample + pop
		} else {
			crackly[i] = sample
		}
	}
	return crackly
}

func applyWowFlutter(samples []float64) []float64 {
	modulated := make([]float64, len(samples))
	wowFreq := 2.0
	flutterFreq := 8.0
	wowDepth := 0.005
	flutterDepth := 0.001
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
	faded := make([]float64, len(samples))
	fadeRate1 := 0.5
	fadeRate2 := 0.15
	fadeRate3 := 1.2
	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)
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
		progress := math.Mod(t, cycleSec) / cycleSec
		if progress < 0.5 {
			progress = progress * 2
		} else {
			progress = 1 - (progress-0.5)*2
		}
		freq := startFreq + (endFreq-startFreq)*progress
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
		pulse := 0.5 + 0.5*math.Sin(2*math.Pi*pulseRate*t)
		carrier := math.Sin(2*math.Pi*freqHz*t)
		buzzer[i] = samples[i] + carrier*pulse*modDepth
	}
	return buzzer
}

func applyRadioNoise(samples []float64) []float64 {
	out := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		t := float64(i) / float64(sampleRate)
		hum := 0.04*math.Sin(2*math.Pi*50*t) + 0.015*math.Sin(2*math.Pi*100*t)
		whistle := 0.008 * math.Sin(2*math.Pi*3200*t)
		hiss := (rand.Float64()*2 - 1) * 0.08
		out[i] = samples[i] + hum + whistle + hiss
	}
	return out
}

func applyWalkieTalkie(samples []float64) []float64 {
	hp := applyHighPassFilter(samples, 600.0)
	lp := applyLowPassFilter(hp, 2200.0)
	out := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		val := lp[i]
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
		val := math.Tanh(samples[i] * 10.0)
		if val > 0.8 {
			val = 0.8
		} else if val < -0.7 {
			val = -0.7
		}
		out[i] = val * 0.9
	}
	return out
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

func executeStreamPipeline(ctx context.Context, cfg Config) error {
	var args []string
	args = append(args, "-re", "-loop", "1", "-i", cfg.BackgroundImage)
	args = append(args, "-f", "s16le", "-ar", "44100", "-ac", "1", "-i", "pipe:0")
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast",
		"-b:v", cfg.VideoBitrate, "-maxrate", cfg.VideoBitrate, "-bufsize", "6000k",
		"-pix_fmt", "yuv420p", "-g", "60", "-r", fmt.Sprintf("%d", cfg.FPS),
		"-c:a", "aac", "-b:a", cfg.AudioBitrate, "-ar", "44100",
		"-vf", fmt.Sprintf("fps=%d,scale=1920:1080,format=yuv420p", cfg.FPS),
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

		for {
			select {
			case <-ctx.Done():
				return
			default:
				audioMutex.RLock()
				samples := audioSamples
				audioMutex.RUnlock()

				if len(samples) == 0 {
					time.Sleep(10 * time.Millisecond)
					continue
				}

				for i := 0; i < chunkSize; i++ {
					sampleIdx := (pos + i) % len(samples)
					val := int16(samples[sampleIdx] * 32767.0)
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(val))
				}

				pos = (pos + chunkSize) % len(samples)

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

func applyPreset(preset string) {
	switch preset {
	case "hizzer":
		audioProc.BuzzerEnabled = true
		audioProc.BuzzerFrequency = 4625.0
		audioProc.BuzzerPulseRate = 1.0
		audioProc.LowPassEnabled = true
		audioProc.LowPassCutoff = 3400.0
		audioProc.HighPassEnabled = true
		audioProc.HighPassCutoff = 300.0
		audioProc.DistortionEnabled = true
		audioProc.DistortionAmount = 0.3
		audioProc.BitCrushEnabled = true
		audioProc.BitCrushDepth = 12
		audioProc.SampleRateReduceEnabled = true
		audioProc.SampleRateReduce = 11025
		audioProc.RingModEnabled = true
		audioProc.RingModFreq = 50.0
		audioProc.AmplitudeModEnabled = true
		audioProc.AmplitudeModFreq = 2.0
		audioProc.ReverbEnabled = true
		audioProc.ReverbAmount = 0.2
		audioProc.DelayEnabled = true
		audioProc.DelayAmount = 0.15
		audioProc.NoiseFloorEnabled = true
		audioProc.NoiseFloor = -40.0
		audioProc.PulseInterference = true
		audioProc.StaticCrackleEnabled = true
		audioProc.StaticCrackle = 0.1
		audioProc.EqEnabled = true
		audioProc.EqBassGain = -3.0
		audioProc.EqMidGain = 2.0
		audioProc.EqTrebleGain = -2.0
		audioProc.CompressorEnabled = true
		audioProc.CompressorThreshold = -12.0
		audioProc.CompressorRatio = 4.0
		audioProc.WowFlutterEnabled = true
		audioProc.RadioFading = true
	case "radio":
		audioProc.BuzzerEnabled = false
		audioProc.LowPassEnabled = true
		audioProc.LowPassCutoff = 5000.0
		audioProc.HighPassEnabled = true
		audioProc.HighPassCutoff = 100.0
		audioProc.DistortionEnabled = true
		audioProc.DistortionAmount = 0.05
		audioProc.NoiseFloorEnabled = true
		audioProc.NoiseFloor = -50.0
		audioProc.RadioNoiseEnabled = true
		audioProc.RadioFading = true
	case "lofi":
		audioProc.BuzzerEnabled = false
		audioProc.LowPassEnabled = true
		audioProc.LowPassCutoff = 8000.0
		audioProc.BitCrushEnabled = true
		audioProc.BitCrushDepth = 8
		audioProc.SampleRateReduceEnabled = true
		audioProc.SampleRateReduce = 22050
		audioProc.DistortionEnabled = true
		audioProc.DistortionAmount = 0.4
		audioProc.ReverbEnabled = true
		audioProc.ReverbAmount = 0.3
		audioProc.WowFlutterEnabled = true
	case "clean":
		audioProc.BuzzerEnabled = false
		audioProc.LowPassEnabled = false
		audioProc.HighPassEnabled = false
		audioProc.DistortionEnabled = false
		audioProc.BitCrushEnabled = false
		audioProc.SampleRateReduceEnabled = false
		audioProc.ReverbEnabled = false
		audioProc.DelayEnabled = false
		audioProc.NoiseFloorEnabled = false
		audioProc.StaticCrackleEnabled = false
		audioProc.EqEnabled = false
		audioProc.CompressorEnabled = false
		audioProc.WowFlutterEnabled = false
		audioProc.RadioFading = false
	}
}

// --- Main Entry Point ---

func main() {
	// CLI flags
	message := flag.String("message", "Hello World", "Morse code message to broadcast")
	background := flag.String("bg", "background.jpg", "Background image path")
	rtmp := flag.String("rtmp", "", "RTMP URL for streaming")
	output := flag.String("output", "", "Output file path (WAV)")
	externalAudio := flag.String("audio", "", "External audio file path (WAV)")
	duration := flag.Int("duration", 0, "Duration in seconds (0 = infinite)")
	preset := flag.String("preset", "hizzer", "Audio preset (hizzer, radio, lofi, clean)")
	noBuzzer := flag.Bool("no-buzzer", false, "Disable buzzer carrier")
	noFilters := flag.Bool("no-filters", false, "Disable all filters")
	noFX := flag.Bool("no-fx", false, "Disable all effects")
	listPresets := flag.Bool("list-presets", false, "List available presets")
	help := flag.Bool("help", false, "Show help")
	
	flag.Parse()

	if *listPresets {
		fmt.Println(titleStyle.Render("Available Presets"))
		fmt.Println()
		fmt.Println(boxStyle.Render(
			labelStyle.Render("hizzer") + " - Authentic HIZZER buzzer sound with all effects\n" +
			labelStyle.Render("radio") + " - Classic AM radio sound with noise and fading\n" +
			labelStyle.Render("lofi") + " - Lo-fi hip hop style with bit crushing and wow/flutter\n" +
			labelStyle.Render("clean") + " - Clean audio with no effects applied",
		))
		return
	}

	if *help {
		fmt.Println(titleStyle.Render("HIZZER Morse Code Broadcast Engine"))
		fmt.Println()
		fmt.Println(boxStyle.Render(
			labelStyle.Render("Usage:") + "\n" +
			"  hizzer [flags]\n\n" +
			labelStyle.Render("Flags:") + "\n" +
			"  -message string     Morse code message to broadcast (default \"CQ CQ DE HIZZER\")\n" +
			"  -bg string          Background image path (default \"background.jpg\")\n" +
			"  -rtmp string        RTMP URL for streaming to platforms like YouTube\n" +
			"  -output string      Output file path (saves as WAV)\n" +
			"  -audio string       External audio file to mix (WAV format)\n" +
			"  -duration int       Duration in seconds (0 = infinite)\n" +
			"  -preset string      Audio preset (hizzer, radio, lofi, clean) (default \"hizzer\")\n" +
			"  -no-buzzer          Disable the buzzer carrier\n" +
			"  -no-filters         Disable all filters (LPF, HPF)\n" +
			"  -no-fx              Disable all effects (distortion, reverb, delay, etc.)\n" +
			"  -list-presets       List available presets\n" +
			"  -help               Show this help\n\n" +
			labelStyle.Render("Examples:") + "\n" +
			"  # Stream to YouTube with HIZZER effects\n" +
			"  hizzer -message \"TEST TEST\" -rtmp rtmp://a.rtmp.youtube.com/live2/STREAM_KEY\n\n" +
			"  # Save to file with clean audio\n" +
			"  hizzer -message \"HELLO WORLD\" -output output.wav -preset clean\n\n" +
			"  # Mix with external audio and stream\n" +
			"  hizzer -message \"CQ CQ\" -audio background_music.wav -rtmp rtmp://...",
		))
		return
	}

	// Apply presets
	if *preset != "" {
		applyPreset(*preset)
	}

	// Override settings with flags
	if *noBuzzer {
		audioProc.BuzzerEnabled = false
	}
	if *noFilters {
		audioProc.LowPassEnabled = false
		audioProc.HighPassEnabled = false
	}
	if *noFX {
		audioProc.DistortionEnabled = false
		audioProc.BitCrushEnabled = false
		audioProc.SampleRateReduceEnabled = false
		audioProc.RingModEnabled = false
		audioProc.AmplitudeModEnabled = false
		audioProc.ReverbEnabled = false
		audioProc.DelayEnabled = false
		audioProc.WowFlutterEnabled = false
		audioProc.RadioFading = false
		audioProc.FilterSweepEnabled = false
	}

	// Check if background exists
	if _, err := os.Stat(*background); os.IsNotExist(err) {
		fmt.Println(infoStyle.Render("Background image not found, creating default..."))
		if err := createDefaultBackground(*background); err != nil {
			fmt.Println(errorStyle.Render("Failed to create default background"))
		}
	}

	// Initialize speaker
	_ = speaker.Init(beep.SampleRate(sampleRate), beep.SampleRate(sampleRate).N(time.Second/10))

	// Create and run TUI model
	cfg := CLIConfig{
		Message:         *message,
		BackgroundImage: *background,
		RTMPURL:         *rtmp,
		OutputFile:      *output,
		ExternalAudio:   *externalAudio,
		Duration:        *duration,
		Preset:          *preset,
	}

	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ECDC4"))

	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	m := model{
		spinner:  s,
		progress: p,
		status:   "Generating audio with effects...",
		ready:    true,
		cfg:      cfg,
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Println(errorStyle.Render(fmt.Sprintf("Error running program: %v", err)))
		os.Exit(1)
	}
}