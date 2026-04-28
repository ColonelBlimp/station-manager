package main

import "image/color"

// inputBorderColor is the 1dp outline drawn around text inputs.
var inputBorderColor = color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0xff}

// statusRowBorderColor is a temporary debug-red used to make the
// status row visible while the layout is being assembled.
var statusRowBorderColor = color.NRGBA{R: 0xff, A: 0xff}

// mainRowBorderColor is a temporary debug-blue used to make the
// main row visible while the layout is being assembled.
var mainRowBorderColor = color.NRGBA{B: 0xff, A: 0xff}

// leftPanelBorderColor is a temporary debug-green used to make the
// 2/3-width left panel of the main row visible.
var leftPanelBorderColor = color.NRGBA{G: 0xff, A: 0xff}

// rightPanelBorderColor is a temporary debug-yellow used to make
// the 1/3-width right panel of the main row visible.
var rightPanelBorderColor = color.NRGBA{R: 0xff, G: 0xff, A: 0xff}

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
