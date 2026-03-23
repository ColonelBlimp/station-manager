# Station Manager: logging application

This is a desktop application for **Linux** (currently) and is used for logging QSOs.

# Exit Codes

This section lists the exit codes of the application.

- 0 = OK
- 100 = Failed to determine the working directory (the same directory in which the application is located).
- 101 = Failed to initialize the Ioc/Di container.

# Development Environment
This section describes the development environment for the application.

The main item here is the `.env` file. This file contains the following environment variables:

`SM_WORKING_DIR` - which is the absolute path to the directory in which the application executable will be located after
building. This is NOT the final location of the application executable. From this location the application executable
should be copied to the final location.

`DEPLOY_DIR` - the final localtion of the application executable. This directory contains the subdirectory `db` where the
sqlite database file is located, and `logs` directory where the log files are located. This directory also contains the
`config.json` file.


