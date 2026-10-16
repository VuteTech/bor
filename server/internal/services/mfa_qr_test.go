// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestRenderTOTPQR(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

	tests := []struct {
		name     string
		username string
		alg      string
	}{
		{name: "plain username", username: "alice", alg: "SHA256"},
		{name: "email username", username: "alice@example.com", alg: "SHA512"},
		{name: "username with reserved characters", username: "a b?c#d&e", alg: "SHA1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := renderTOTPQR(tt.username, tt.alg, secret)
			if err != nil {
				t.Fatalf("renderTOTPQR: %v", err)
			}

			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode png: %v", err)
			}

			want := qrImageSize + 2*qrQuietZone
			if b := img.Bounds(); b.Dx() != want || b.Dy() != want {
				t.Fatalf("image size = %dx%d, want %dx%d", b.Dx(), b.Dy(), want, want)
			}

			// The quiet zone must be white on every edge.
			for _, p := range []image.Point{
				{0, 0}, {want - 1, 0}, {0, want - 1}, {want - 1, want - 1},
				{qrQuietZone - 1, want / 2}, {want / 2, qrQuietZone - 1},
			} {
				if g := color.GrayModel.Convert(img.At(p.X, p.Y)).(color.Gray); g.Y != 0xff {
					t.Errorf("pixel %v = %d, want white", p, g.Y)
				}
			}

		})
	}
}
