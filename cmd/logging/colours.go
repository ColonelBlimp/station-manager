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
