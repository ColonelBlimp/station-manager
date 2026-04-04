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

## Setting permissions

The group `uucp` or `dialout` is the group that *owns* the device. You need to add your user to this group to allow access.
To do this, enter the following command:

     sudo usermod -a -G [group_name] $USER

Enter the following command to verify that you are now a member of the group:

     id $USER

You should see the group name listed. If not, you may need to log out and log back in
for the changes to take effect.

## Configuration opions

The `config.json` file has a `rig_configs` section, with a subsection for each rig. In turn, each rig has a `SerialConfig`
option, where you can specify the baud rate, data bits, stop bits, and parity. While the file can be edited manually,
the Station Manager has a configuration application that can be used to make changes.