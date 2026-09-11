package userprofile

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

const (
	// avatarOutputSize is the side length (pixels) of every normalized avatar.
	avatarOutputSize = 512
	// avatarJPEGQuality is the JPEG quality used for opaque avatars.
	avatarJPEGQuality = 85
)

// normalizedAvatar is the result of re-encoding an uploaded image: the encoded
// bytes plus the storage extension and content type matching that encoding.
type normalizedAvatar struct {
	data        []byte
	ext         string
	contentType string
}

// normalizeAvatar decodes an uploaded image, center-crops it to a square,
// scales it to avatarOutputSize×avatarOutputSize and re-encodes it. The output
// is PNG when the source has transparency (alpha), otherwise JPEG. Re-encoding
// writes a fresh stream, which drops EXIF/GPS and any other source metadata.
//
// Animated GIFs are decoded via the standard image/gif decoder, which returns
// the first embedded frame, so animated sources are flattened to a single
// static frame.
func normalizeAvatar(data []byte) (*normalizedAvatar, error) {
	src, err := decodeAvatar(data)
	if err != nil {
		return nil, err
	}

	// Detect alpha on the decoded source, before compositing, so a transparent
	// source is preserved as PNG rather than flattened onto a black background.
	transparent := hasAlpha(src)

	square := centerCropSquare(src)
	dst := image.NewRGBA(image.Rect(0, 0, avatarOutputSize, avatarOutputSize))
	// CatmullRom is a high-quality resampler from golang.org/x/image/draw and
	// handles both up- and down-scaling.
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Over, nil)

	if transparent {
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, err
		}
		return &normalizedAvatar{data: buf.Bytes(), ext: ".png", contentType: "image/png"}, nil
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: avatarJPEGQuality}); err != nil {
		return nil, err
	}
	return &normalizedAvatar{data: buf.Bytes(), ext: ".jpg", contentType: "image/jpeg"}, nil
}

// decodeAvatar decodes the first frame of the encoded image. The webp decoder
// is registered by the package (see handler.go) so all supported upload types
// are decodable here. For GIFs the standard decoder returns the first frame.
func decodeAvatar(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// centerCropSquare returns the largest centered square sub-image of src,
// preserving the source pixel data (no scaling).
func centerCropSquare(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	size := w
	if h < size {
		size = h
	}
	x0 := b.Min.X + (w-size)/2
	y0 := b.Min.Y + (h-size)/2
	rect := image.Rect(x0, y0, x0+size, y0+size)

	if sub, ok := src.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(rect)
	}

	// Fallback for images without a SubImage fast path: copy the crop region.
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
	return dst
}

// hasAlpha reports whether the image contains any pixel that is not fully
// opaque. It scans the decoded source and short-circuits on the first
// transparent pixel.
func hasAlpha(img image.Image) bool {
	switch m := img.(type) {
	case *image.NRGBA:
		return anyAlphaByte(m.Pix, 4, 3)
	case *image.RGBA:
		return anyAlphaByte(m.Pix, 4, 3)
	case *image.NRGBA64:
		return anyAlphaByte16(m.Pix, 8, 6)
	case *image.RGBA64:
		return anyAlphaByte16(m.Pix, 8, 6)
	case *image.Paletted:
		for _, c := range m.Palette {
			if _, _, _, a := c.RGBA(); a != 0xffff {
				return true
			}
		}
		return false
	}

	// Generic fallback covers the remaining standard types (YCbCr, Gray,
	// Gray16, CMYK, NYCbCrA, ...) and any custom decoder output.
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
				return true
			}
		}
	}
	return false
}

// anyAlphaByte scans a byte-per-channel pixel slice for any alpha channel value
// below 0xff. stride is the number of bytes per pixel and offset the index of
// the alpha channel within a pixel.
func anyAlphaByte(pix []byte, stride, offset int) bool {
	for i := offset; i < len(pix); i += stride {
		if pix[i] != 0xff {
			return true
		}
	}
	return false
}

// anyAlphaByte16 is anyAlphaByte for 16-bit-per-channel pixels.
func anyAlphaByte16(pix []byte, stride, offset int) bool {
	for i := offset; i+1 < len(pix); i += stride {
		if pix[i] != 0xff || pix[i+1] != 0xff {
			return true
		}
	}
	return false
}
