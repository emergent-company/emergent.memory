#!/usr/bin/env python3
"""Record audio from the default mic for use in replay tests.

Usage (on the mac mini):
    python tools/record_audio.py --duration 8 --output test_commands/desk_light.pcm
    # then say "hey alfred turn on the desk light" during the 8s window

Output: raw int16 PCM, 16kHz, mono — the exact format Gemini expects.
"""

import argparse

import numpy as np
import pyaudio

SAMPLE_RATE = 16000
FRAME_SAMPLES = 800  # 50ms


def main() -> None:
    p = argparse.ArgumentParser(description="Record audio for replay tests")
    p.add_argument("--duration", type=float, default=8, help="recording length in seconds")
    p.add_argument("--rate", type=int, default=SAMPLE_RATE, help="sample rate in Hz")
    p.add_argument("--output", "-o", required=True, help="output .pcm file")
    p.add_argument("--device", type=int, help="PyAudio input device index (default: system default)")
    args = p.parse_args()

    pa = pyaudio.PyAudio()

    device_idx = args.device
    if device_idx is None:
        info = pa.get_default_input_device_info()
        device_idx = info["index"]
        print(f"Using default input device: {info['name']} (index {device_idx})")

    stream = pa.open(
        format=pyaudio.paInt16,
        channels=1,
        rate=args.rate,
        input=True,
        input_device_index=device_idx,
        frames_per_buffer=FRAME_SAMPLES,
    )

    total_samples = int(args.duration * args.rate)
    frames: list[bytes] = []
    print(f"Recording {args.duration}s at {args.rate}Hz → {args.output}", flush=True)

    collected = 0
    try:
        while collected < total_samples:
            data = stream.read(FRAME_SAMPLES, exception_on_overflow=False)
            frames.append(data)
            collected += FRAME_SAMPLES
            elapsed = collected / args.rate
            bar = "█" * int(elapsed / args.duration * 30)
            print(f"\r[{bar:<30s}] {elapsed:.1f}s", end="", flush=True)
    except KeyboardInterrupt:
        print("\nRecording stopped early", flush=True)
    finally:
        stream.stop_stream()
        stream.close()
        pa.terminate()

    with open(args.output, "wb") as f:
        for chunk in frames:
            f.write(chunk)
    size = sum(len(c) for c in frames)
    print(f"\nSaved {size} bytes ({size/args.rate:.1f}s) → {args.output}", flush=True)

    # Quick stats
    all_samples = np.frombuffer(b"".join(frames), dtype=np.int16)
    rms = float(np.sqrt(np.mean(all_samples.astype(np.float64) ** 2)))
    peak = int(np.max(np.abs(all_samples)))
    print(f"RMS: {rms:.0f}  peak: {peak}  {'✅ loud enough' if rms > 100 else '⚠️  very quiet — check mic'}")


if __name__ == "__main__":
    main()
