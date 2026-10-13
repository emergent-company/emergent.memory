## ADDED Requirements

### Requirement: Connected devices page confirms a pushed server install

The connected devices page SHALL surface a success flash when it is reached from
a device install, confirming that a server was installed on the device.

#### Scenario: Device install lands on the devices page

- **WHEN** the connected devices page is loaded with the device-install success parameter
- **THEN** it shows a flash confirming the server was installed on the device
