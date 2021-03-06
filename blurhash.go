// Copyright 2021-2022 Nao Yonashiro
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package blurhash

import (
	"image"
	"image/color"
	"math"
)

func init() {
	buildLinearTable()
}

func Append(dst []byte, img image.Image, w, h int) []byte {
	factors := make([]factor, 81)[:w*h]

	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	piW := math.Pi / float64(imgW)
	piH := math.Pi / float64(imgH)

	yCos := make([]float64, 9)[:h]
	ySin := make([]float64, 9)[:h]
	yRotCos := make([]float64, 9)[:h]
	yRotSin := make([]float64, 9)[:h]
	for i := 0; i < h; i++ {
		yRotSin[i], yRotCos[i] = math.Sincos(piH * float64(i))
	}

	xCos := make([]float64, 9)[:w]
	xSin := make([]float64, 9)[:w]
	xRotCos := make([]float64, 9)[:w]
	xRotSin := make([]float64, 9)[:w]
	for j := 0; j < w; j++ {
		xRotSin[j], xRotCos[j] = math.Sincos(piW * float64(j))
	}

	fastAt := fastAccessor(img)
	for y := 0; y < imgH; y++ {
		for i := range yCos {
			if y == 0 || i == 0 {
				ySin[i], yCos[i] = 0, 1
			} else {
				ySin[i], yCos[i] = rotate(ySin[i], yCos[i], yRotSin[i], yRotCos[i])
			}
		}
		for x := 0; x < imgW; x++ {
			for j := range xCos {
				if x == 0 || j == 0 {
					xSin[j], xCos[j] = 0, 1
				} else {
					xSin[j], xCos[j] = rotate(xSin[j], xCos[j], xRotSin[j], xRotCos[j])
				}
			}

			pR, pG, pB, _ := fastAt(x, y)
			r := sRGB((pR >> 8) & 0xff).linear()
			g := sRGB((pG >> 8) & 0xff).linear()
			b := sRGB((pB >> 8) & 0xff).linear()

			for i := 0; i < h; i++ {
				for j := 0; j < w; j++ {
					basis := yCos[i] * xCos[j]
					factors[i*w+j].r += basis * r
					factors[i*w+j].g += basis * g
					factors[i*w+j].b += basis * b
				}
			}
		}
	}

	dc := factors[0]
	dc.Scale(1 / float64(imgH*imgW))

	ac := factors[1:]
	for i := range ac {
		ac[i].Scale(2 / float64(imgH*imgW))
	}

	packedShape := (h-1)*9 + (w - 1)
	dst = append1Base83(dst, packedShape)
	var max float64
	if len(ac) > 0 {
		actualMax := float64(0)
		for _, f := range ac {
			actualMax = math.Max(math.Abs(f.r), actualMax)
			actualMax = math.Max(math.Abs(f.g), actualMax)
			actualMax = math.Max(math.Abs(f.b), actualMax)
		}
		quantisedMax := int(clamp(0, 82, math.Floor(actualMax*166-0.5)))
		max = float64(quantisedMax+1) / 166
		dst = append1Base83(dst, quantisedMax)
	} else {
		max = 1
		dst = append1Base83(dst, 0)
	}
	dst = append4Base83(dst, encodeDC(dc))
	for i := range ac {
		dst = append2Base83(dst, encodeAC(ac[i], max))
	}
	return dst
}

func Encode(img image.Image, w, h int) string {
	dst := make([]byte, 0, EncodedLen(w, h))
	return string(Append(dst, img, w, h))
}

func EncodedLen(w, h int) int {
	packedShapeBytes := 1
	maxValueBytes := 1
	dcBytes := 4
	acBytes := (w*h - 1) * 2
	return packedShapeBytes + maxValueBytes + dcBytes + acBytes
}

type factor struct {
	r, g, b float64
}

func (f *factor) Scale(v float64) {
	f.r *= v
	f.g *= v
	f.b *= v
}

func encodeDC(dc factor) int {
	roundedR := int(linear(dc.r).sRGB())
	roundedG := int(linear(dc.g).sRGB())
	roundedB := int(linear(dc.b).sRGB())
	return (roundedR << 16) | (roundedG << 8) | roundedB
}

func encodeAC(ac factor, max float64) int {
	quantR := int(clamp(0, 18, math.Floor(signSqrt(ac.r/max)*9+9.5)))
	quantG := int(clamp(0, 18, math.Floor(signSqrt(ac.g/max)*9+9.5)))
	quantB := int(clamp(0, 18, math.Floor(signSqrt(ac.b/max)*9+9.5)))
	return quantR*(19*19) + quantG*19 + quantB
}

var linearTable [256]float64

func buildLinearTable() {
	for i := 0; i < 256; i++ {
		v := float64(i) / 255
		if v <= 0.04045 {
			linearTable[i] = v / 12.92
		} else {
			linearTable[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
}

type sRGB uint8

func (value sRGB) linear() float64 {
	return linearTable[value]
}

type linear float64

func (value linear) sRGB() uint8 {
	v := clamp(0, 1, float64(value))
	if v <= 0.0031308 {
		return uint8(clamp(0, 255, v*12.92*255+0.5))
	} else {
		return uint8(clamp(0, 255, 1.055*math.Pow(v, 1/2.4)-0.055)*255 + 0.5)
	}
}

func clamp(min, max, x float64) float64 {
	return math.Max(min, math.Min(max, x))
}

func signSqrt(value float64) float64 {
	return math.Copysign(math.Sqrt(math.Abs(value)), value)
}

var base83chars = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~")

func append1Base83(dst []byte, v int) []byte {
	return append(dst, base83chars[v%83])
}

func append2Base83(dst []byte, v int) []byte {
	return append1Base83(append1Base83(dst, v/83), v)
}

func append4Base83(dst []byte, v int) []byte {
	return append2Base83(append2Base83(dst, v/(83*83)), v)
}

func fastAccessor(img image.Image) func(x, y int) (r, b, g, a uint32) {
	switch img := img.(type) {
	case *image.YCbCr:
		var yShift, xShift uint8
		switch img.SubsampleRatio {
		case image.YCbCrSubsampleRatio422:
			xShift = 1
		case image.YCbCrSubsampleRatio420:
			yShift, xShift = 1, 1
		case image.YCbCrSubsampleRatio440:
			yShift = 1
		case image.YCbCrSubsampleRatio411:
			xShift = 2
		case image.YCbCrSubsampleRatio410:
			yShift, xShift = 1, 2
		}
		return func(x, y int) (r, b, g, a uint32) {
			yi := img.YOffset(x, y)
			ci := ((y>>yShift)-(img.Rect.Min.Y>>yShift))*img.CStride + ((x >> xShift) - (img.Rect.Min.X >> xShift))
			return color.YCbCr{Y: img.Y[yi], Cb: img.Cb[ci], Cr: img.Cr[ci]}.RGBA()
		}
	case *image.NRGBA:
		return func(x, y int) (r, b, g, a uint32) {
			i := img.PixOffset(x, y)
			s := img.Pix[i : i+4 : i+4]
			return color.NRGBA{R: s[0], G: s[1], B: s[2], A: s[3]}.RGBA()
		}
	default:
		return func(x, y int) (r, b, g, a uint32) {
			return img.At(x, y).RGBA()
		}
	}
}

func rotate(sinA, cosA, sinB, cosB float64) (float64, float64) {
	return sinA*cosB + cosA*sinB, cosA*cosB - sinA*sinB
}
