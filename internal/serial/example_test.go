package serial_test

import (
	"context"
	"fmt"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/serial"
)

func Example() {
	cfg := serial.Config{
		PortName: "/dev/ttyUSB0",
		BaudRate: 9600,
		DataBits: 8,
	}

	client, err := serial.Open(cfg)
	if err != nil {
		fmt.Println("open error:", err)
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	resp, err := client.ExecBytes(ctx, []byte("FA"))
	if err != nil {
		fmt.Println("exec error:", err)
		return
	}

	fmt.Println("response:", string(resp))
}
