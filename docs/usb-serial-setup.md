# Setting Up The USB Serial Port

For Computer Aided Transciever (CAT) control, Station Manager uses the USB serial port. If you are using a Yaesu rig,
you will need to install the CP210x USB to UART Bridge VCP Driver, which is maintained in the Linux kernel tree.
Simply switching on your Yaesu rig should suffice. If you are not ready for this, disable the serial port in the
`config.json` file. Remember, your PC should be on, and you should be logged in **before** connecting the Yaesu rig.

If you are getting *permission errors,* you will need to modify your system to allow access:

## Finding the Device

From a console enter the following command:

     ls -al /dev/ttyUSB0

This should output something like this (Arch Linux):

    crw-rw---- 1 root uucp 188, 0 Dec 11 14:41 /dev/ttyUSB0

or (Fedora)

    crw-rw---- 1 root dialout 188, 0 Dec 11 14:41 /dev/ttyUSB0

## Identifying Enhanced vs Standard Ports (Dual CP210x)

Yaesu rigs such as the FTdx10 and FT-710 use a **Dual CP210x** USB-to-UART bridge, which exposes two serial interfaces:

| USB Interface | Windows Name     | Linux (typically) | Purpose          |
|---------------|------------------|-------------------|------------------|
| Interface 0   | Enhanced COM Port | Lower ttyUSB (e.g. `ttyUSB0`) | **CAT control**  |
| Interface 1   | Standard COM Port | Higher ttyUSB (e.g. `ttyUSB1`) | Audio/data       |

On Windows, Device Manager labels these clearly:

    Silicon Labs Dual CP210x USB to UART Bridge : Enhanced COM Port (COM**)
    Silicon Labs Dual CP210x USB to UART Bridge : Standard COM Port (COM**)

On Linux, both appear as generic `ttyUSB` devices. To identify which is which, use `udevadm` to check the USB interface number:

     udevadm info -a /dev/ttyUSB0 | grep bInterfaceNumber

If the output includes something like `ATTRS{bInterfaceNumber}=="00"`, that device is the **Enhanced** port (Interface 0 — used for CAT control). If it shows `"01"`, it is the **Standard** port (Interface 1).
On some systems (Fedora), the Enhanced port may appear as `ATTRS{bNumInterfaces}==" 0"` and the Standard port as `ATTRS{bNumInterfaces}==" 1"`.

You can also check `dmesg` immediately after plugging in the rig:

     dmesg | grep -i cp210x | tail

This will show lines like:

    cp210x 1-2:1.0: cp210x converter detected
    cp210x converter now attached to ttyUSB0
    cp210x 1-2:1.1: cp210x converter detected
    cp210x converter now attached to ttyUSB1

The `1.0` suffix is Interface 0 (Enhanced / CAT), and `1.1` is Interface 1 (Standard).

> **Note:** The ttyUSB numbering depends on which other USB serial devices are connected.
> If you have other devices plugged in, the numbers may shift. Always verify with `udevadm` or `dmesg`
> rather than assuming `ttyUSB0` is the Enhanced port.

For CAT control in Station Manager, configure the **Enhanced** port (Interface 0) in `config.json` under `rig_configs` → `SerialConfig` → `PortName`.

## Setting permissions

The group `uucp` or `dialout` is the group that *owns* the device. You need to add your user to this group to allow access.
To do this, enter the following command:

     sudo usermod -a -G [group_name] $USER

Enter the following command to verify that you are now a member of the group:

     id $USER

You should see the group name listed. If not, you may need to reboot for the changes to take effect.

## Configuration options

The `config.json` file has a `rig_configs` section, with a subsection for each rig. In turn, each rig has a `SerialConfig`
option, where you can specify the baud rate, data bits, stop bits, and parity. While the file can be edited manually,
the Station Manager has a configuration application that can be used to make changes.

# Troubleshooting

## Wrong port selected

If CAT commands are not getting responses, you may have the Standard port configured instead of the Enhanced port.
Verify the interface number with `udevadm info -a /dev/ttyUSBx | grep bInterfaceNumber` and ensure Interface 0
(Enhanced) is set as the `PortName` in your rig's `SerialConfig`.
