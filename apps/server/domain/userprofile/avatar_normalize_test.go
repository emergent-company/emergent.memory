package userprofile

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// ---------------------------------------------------------------------------
// Normalization fixtures / helpers (no DB or storage required)
// ---------------------------------------------------------------------------

func mustEncodeTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func mustEncodeTestJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func mustEncodeTestGIF(t *testing.T, frames []*image.Paletted) []byte {
	t.Helper()
	delays := make([]int, len(frames))
	for i := range delays {
		delays[i] = 10
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: delays, LoopCount: 0}); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func decodeNormalized(t *testing.T, raw []byte) (image.Image, string) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("output does not decode: %v", err)
	}
	return img, format
}

func rgb8(img image.Image, x, y int) (r, g, b, a uint32) {
	r, g, b, a = img.At(x, y).RGBA()
	return r >> 8, g >> 8, b >> 8, a >> 8
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestNormalizeAvatar_NonSquareJPEG_CenterCropsToSquare verifies that a wide
// JPEG is center-cropped (vertically-oriented stripes: the crop keeps the
// middle band) and re-encoded as a 512x512 JPEG that is not byte-equal to the
// input (i.e. it was re-encoded).
func TestNormalizeAvatar_NonSquareJPEG_CenterCropsToSquare(t *testing.T) {
	const w, h = 200, 100
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			switch {
			case x < 50:
				src.Set(x, y, color.NRGBA{R: 255, A: 255}) // left band: red
			case x < 150:
				src.Set(x, y, color.NRGBA{G: 255, A: 255}) // middle band: green
			default:
				src.Set(x, y, color.NRGBA{B: 255, A: 255}) // right band: blue
			}
		}
	}
	input := mustEncodeTestJPEG(t, src)

	got, err := normalizeAvatar(input)
	if err != nil {
		t.Fatalf("normalizeAvatar: %v", err)
	}
	if got.ext != ".jpg" || got.contentType != "image/jpeg" {
		t.Fatalf("got ext=%q contentType=%q, want .jpg/image/jpeg", got.ext, got.contentType)
	}

	img, format := decodeNormalized(t, got.data)
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	b := img.Bounds()
	if b.Dx() != avatarOutputSize || b.Dy() != avatarOutputSize {
		t.Fatalf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), avatarOutputSize, avatarOutputSize)
	}

	// The entire center crop is the green middle band, so corners and center
	// must read green.
	for name, pt := range map[string][2]int{
		"top-left":     {2, 2},
		"top-right":    {avatarOutputSize - 3, 2},
		"bottom-left":  {2, avatarOutputSize - 3},
		"bottom-right": {avatarOutputSize - 3, avatarOutputSize - 3},
		"center":       {avatarOutputSize / 2, avatarOutputSize / 2},
	} {
		r, g, bl, _ := rgb8(img, pt[0], pt[1])
		if g <= r+30 || g <= bl+30 {
			t.Errorf("%s: rgb=(%d,%d,%d), want green-dominant after center crop", name, r, g, bl)
		}
	}

	if bytes.Equal(got.data, input) {
		t.Errorf("output equals input; image was not re-encoded")
	}
}

// TestNormalizeAvatar_TransparentPNG_KeepsAlpha verifies that a transparent
// source is re-encoded as PNG with the alpha channel preserved, even though the
// source is not the same dimensions as the output.
func TestNormalizeAvatar_TransparentPNG_KeepsAlpha(t *testing.T) {
	const size = 100
	src := image.NewNRGBA(image.Rect(0, 0, size, size))
	// Fully transparent background with an opaque red square in the middle.
	for y := 25; y < 75; y++ {
		for x := 25; x < 75; x++ {
			src.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	input := mustEncodeTestPNG(t, src)

	got, err := normalizeAvatar(input)
	if err != nil {
		t.Fatalf("normalizeAvatar: %v", err)
	}
	if got.ext != ".png" || got.contentType != "image/png" {
		t.Fatalf("got ext=%q contentType=%q, want .png/image/png", got.ext, got.contentType)
	}

	img, format := decodeNormalized(t, got.data)
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if b := img.Bounds(); b.Dx() != avatarOutputSize || b.Dy() != avatarOutputSize {
		t.Fatalf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), avatarOutputSize, avatarOutputSize)
	}

	// Corner stays transparent; center stays opaque red.
	if _, _, _, a := rgb8(img, 2, 2); a != 0 {
		t.Errorf("corner alpha = %d, want 0 (transparency not preserved)", a)
	}
	r, g, b, a := rgb8(img, avatarOutputSize/2, avatarOutputSize/2)
	if a != 255 {
		t.Errorf("center alpha = %d, want 255", a)
	}
	if r <= g+30 || r <= b+30 {
		t.Errorf("center rgb=(%d,%d,%d), want red-dominant", r, g, b)
	}
}

// TestNormalizeAvatar_AnimatedGIF_FlattensToFirstFrame verifies that an
// animated GIF becomes a single static image taken from the first frame.
func TestNormalizeAvatar_AnimatedGIF_FlattensToFirstFrame(t *testing.T) {
	red := color.Palette{color.RGBA{R: 255, A: 255}}
	blue := color.Palette{color.RGBA{B: 255, A: 255}}
	frame0 := image.NewPaletted(image.Rect(0, 0, 50, 50), red)
	frame1 := image.NewPaletted(image.Rect(0, 0, 50, 50), blue)
	input := mustEncodeTestGIF(t, []*image.Paletted{frame0, frame1})

	// Sanity-check that the input really is animated.
	decoded, err := gif.DecodeAll(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("decode input gif: %v", err)
	}
	if len(decoded.Image) < 2 {
		t.Fatalf("input gif has %d frames, want >= 2", len(decoded.Image))
	}

	got, err := normalizeAvatar(input)
	if err != nil {
		t.Fatalf("normalizeAvatar: %v", err)
	}

	img, format := decodeNormalized(t, got.data)
	if format == "gif" {
		t.Fatalf("output format = gif, want a single static frame (png/jpeg)")
	}
	if b := img.Bounds(); b.Dx() != avatarOutputSize || b.Dy() != avatarOutputSize {
		t.Fatalf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), avatarOutputSize, avatarOutputSize)
	}
	// First frame is red; if a later frame were used the pixel would be blue.
	r, g, b, _ := rgb8(img, avatarOutputSize/2, avatarOutputSize/2)
	if r <= g+30 || r <= b+30 {
		t.Errorf("center rgb=(%d,%d,%d), want red-dominant (first frame)", r, g, b)
	}
}

// TestNormalizeAvatar_SmallSquare_UpscalesTo512 verifies that a square image
// smaller than the output is still scaled up to 512x512.
func TestNormalizeAvatar_SmallSquare_UpscalesTo512(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			src.Set(x, y, color.NRGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	got, err := normalizeAvatar(mustEncodeTestJPEG(t, src))
	if err != nil {
		t.Fatalf("normalizeAvatar: %v", err)
	}
	img, _ := decodeNormalized(t, got.data)
	if b := img.Bounds(); b.Dx() != avatarOutputSize || b.Dy() != avatarOutputSize {
		t.Fatalf("dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), avatarOutputSize, avatarOutputSize)
	}
}

// TestService_UploadAvatar_StoresNormalizedImage exercises the normalization
// pipeline through the service and asserts the stored object is re-encoded at
// 512x512 rather than the raw upload.
func TestService_UploadAvatar_StoresNormalizedImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 300, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 300; x++ {
			src.Set(x, y, color.NRGBA{G: 180, A: 255})
		}
	}
	input := mustEncodeTestJPEG(t, src)

	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	svc := newTestAvatarService(repo, store)

	dto, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader(input), int64(len(input)), "image/jpeg")
	if err != nil {
		t.Fatalf("UploadAvatar: %v", err)
	}
	if dto.AvatarObjectKey == nil {
		t.Fatal("expected avatar object key")
	}
	stored, ok := store.objects[*dto.AvatarObjectKey]
	if !ok {
		t.Fatalf("object %q not stored", *dto.AvatarObjectKey)
	}
	if bytes.Equal(stored, input) {
		t.Errorf("stored object equals raw upload; not normalized")
	}
	img, format := decodeNormalized(t, stored)
	if format != "jpeg" {
		t.Fatalf("stored format = %q, want jpeg", format)
	}
	if b := img.Bounds(); b.Dx() != avatarOutputSize || b.Dy() != avatarOutputSize {
		t.Fatalf("stored dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), avatarOutputSize, avatarOutputSize)
	}
}
