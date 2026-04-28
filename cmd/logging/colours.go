package main

import "image/color"

// Tailwind CSS v4 indigo palette, sRGB approximations of the
// canonical oklch() values published in the Tailwind theme. Each
// entry is the hex equivalent listed in the v4 docs (browsers
// gamut-map the oklch to roughly these RGB values; exact pixels can
// drift by 1–2 levels depending on the platform's gamut-mapping).
//
// Canonical oklch(L C H) per shade:
//
//	50:  oklch(0.962 0.018 272.314)
//	100: oklch(0.930 0.034 272.788)
//	200: oklch(0.870 0.065 274.039)
//	300: oklch(0.785 0.115 274.713)
//	400: oklch(0.673 0.182 276.935)
//	500: oklch(0.585 0.233 277.117)
//	600: oklch(0.511 0.262 276.966)
//	700: oklch(0.457 0.240 277.023)
//	800: oklch(0.398 0.195 277.366)
//	900: oklch(0.359 0.144 278.697)
//	950: oklch(0.257 0.090 281.288)
var (
	indigo50  = color.NRGBA{R: 0xee, G: 0xf2, B: 0xff, A: 0xff}
	indigo100 = color.NRGBA{R: 0xe0, G: 0xe7, B: 0xff, A: 0xff}
	indigo200 = color.NRGBA{R: 0xc7, G: 0xd2, B: 0xfe, A: 0xff}
	indigo300 = color.NRGBA{R: 0xa5, G: 0xb4, B: 0xfc, A: 0xff}
	indigo400 = color.NRGBA{R: 0x81, G: 0x8c, B: 0xf8, A: 0xff}
	indigo500 = color.NRGBA{R: 0x63, G: 0x66, B: 0xf1, A: 0xff}
	indigo600 = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0xff}
	indigo700 = color.NRGBA{R: 0x43, G: 0x38, B: 0xca, A: 0xff}
	indigo800 = color.NRGBA{R: 0x37, G: 0x30, B: 0xa3, A: 0xff}
	indigo900 = color.NRGBA{R: 0x31, G: 0x2e, B: 0x81, A: 0xff}
	indigo950 = color.NRGBA{R: 0x1e, G: 0x1b, B: 0x4b, A: 0xff}
)

// Tailwind CSS v4 red palette, sRGB approximations of the canonical
// oklch() values published in the Tailwind theme.
//
// Canonical oklch(L C H) per shade:
//
//	50:  oklch(0.971 0.013 17.380)
//	100: oklch(0.936 0.032 17.717)
//	200: oklch(0.885 0.062 18.334)
//	300: oklch(0.808 0.114 19.571)
//	400: oklch(0.704 0.191 22.216)
//	500: oklch(0.637 0.237 25.331)
//	600: oklch(0.577 0.245 27.325)
//	700: oklch(0.505 0.213 27.518)
//	800: oklch(0.444 0.177 26.899)
//	900: oklch(0.396 0.141 25.723)
//	950: oklch(0.258 0.092 26.042)
var (
	red50  = color.NRGBA{R: 0xfe, G: 0xf2, B: 0xf2, A: 0xff}
	red100 = color.NRGBA{R: 0xfe, G: 0xe2, B: 0xe2, A: 0xff}
	red200 = color.NRGBA{R: 0xfe, G: 0xca, B: 0xca, A: 0xff}
	red300 = color.NRGBA{R: 0xfc, G: 0xa5, B: 0xa5, A: 0xff}
	red400 = color.NRGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff}
	red500 = color.NRGBA{R: 0xef, G: 0x44, B: 0x44, A: 0xff}
	red600 = color.NRGBA{R: 0xdc, G: 0x26, B: 0x26, A: 0xff}
	red700 = color.NRGBA{R: 0xb9, G: 0x1c, B: 0x1c, A: 0xff}
	red800 = color.NRGBA{R: 0x99, G: 0x1b, B: 0x1b, A: 0xff}
	red900 = color.NRGBA{R: 0x7f, G: 0x1d, B: 0x1d, A: 0xff}
	red950 = color.NRGBA{R: 0x45, G: 0x0a, B: 0x0a, A: 0xff}
)

// Tailwind CSS v4 gray palette, sRGB approximations of the canonical
// oklch() values published in the Tailwind theme.
//
// Canonical oklch(L C H) per shade:
//
//	50:  oklch(0.985 0.002 247.839)
//	100: oklch(0.967 0.003 264.542)
//	200: oklch(0.928 0.006 264.531)
//	300: oklch(0.872 0.010 258.338)
//	400: oklch(0.707 0.022 261.325)
//	500: oklch(0.551 0.027 264.364)
//	600: oklch(0.446 0.030 256.802)
//	700: oklch(0.373 0.034 259.733)
//	800: oklch(0.278 0.033 256.848)
//	900: oklch(0.210 0.034 264.665)
//	950: oklch(0.130 0.028 261.692)
var (
	gray50  = color.NRGBA{R: 0xf9, G: 0xfa, B: 0xfb, A: 0xff}
	gray100 = color.NRGBA{R: 0xf3, G: 0xf4, B: 0xf6, A: 0xff}
	gray200 = color.NRGBA{R: 0xe5, G: 0xe7, B: 0xeb, A: 0xff}
	gray300 = color.NRGBA{R: 0xd1, G: 0xd5, B: 0xdc, A: 0xff}
	gray400 = color.NRGBA{R: 0x99, G: 0xa1, B: 0xaf, A: 0xff}
	gray500 = color.NRGBA{R: 0x6a, G: 0x72, B: 0x82, A: 0xff}
	gray600 = color.NRGBA{R: 0x4a, G: 0x55, B: 0x65, A: 0xff}
	gray700 = color.NRGBA{R: 0x37, G: 0x41, B: 0x51, A: 0xff}
	gray800 = color.NRGBA{R: 0x1e, G: 0x29, B: 0x39, A: 0xff}
	gray900 = color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0xff}
	gray950 = color.NRGBA{R: 0x03, G: 0x07, B: 0x12, A: 0xff}
)

// Tailwind CSS v4 green palette, sRGB approximations of the canonical
// oklch() values published in the Tailwind theme.
//
// Canonical oklch(L C H) per shade:
//
//	50:  oklch(0.982 0.018 155.826)
//	100: oklch(0.962 0.044 156.743)
//	200: oklch(0.925 0.084 155.995)
//	300: oklch(0.871 0.150 154.449)
//	400: oklch(0.792 0.209 151.711)
//	500: oklch(0.723 0.219 149.579)
//	600: oklch(0.627 0.194 149.214)
//	700: oklch(0.527 0.154 150.069)
//	800: oklch(0.448 0.119 151.328)
//	900: oklch(0.393 0.095 152.535)
//	950: oklch(0.266 0.065 152.934)
var (
	green50  = color.NRGBA{R: 0xf0, G: 0xfd, B: 0xf4, A: 0xff}
	green100 = color.NRGBA{R: 0xdc, G: 0xfc, B: 0xe7, A: 0xff}
	green200 = color.NRGBA{R: 0xbb, G: 0xf7, B: 0xd0, A: 0xff}
	green300 = color.NRGBA{R: 0x86, G: 0xef, B: 0xac, A: 0xff}
	green400 = color.NRGBA{R: 0x4a, G: 0xde, B: 0x80, A: 0xff}
	green500 = color.NRGBA{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff}
	green600 = color.NRGBA{R: 0x16, G: 0xa3, B: 0x4a, A: 0xff}
	green700 = color.NRGBA{R: 0x15, G: 0x80, B: 0x3d, A: 0xff}
	green800 = color.NRGBA{R: 0x16, G: 0x65, B: 0x34, A: 0xff}
	green900 = color.NRGBA{R: 0x14, G: 0x53, B: 0x2d, A: 0xff}
	green950 = color.NRGBA{R: 0x05, G: 0x2e, B: 0x16, A: 0xff}
)
