![img.png](osx-allow-connect.png)

```shell

# Scan for devices
./blim scan --allow e20e664a4716aba3abc6b9a0329b5b2e
NAME  ADDRESS  RSSI  SERVICES  LAST SEEN
--------------------------------------------------------------------------------
ESP32-S3-BLIM-Tes...  e20e664a4716aba3abc6b9a0329b5b2e  -24 dBm  180a,180f,180d,181a,6e40000...  0s ago


# Inspect a device
./blim inspect e20e664a4716aba3abc6b9a0329b5b2e
Device info:
  ID: e20e664a4716aba3abc6b9a0329b5b2e
  Address: e20e664a4716aba3abc6b9a0329b5b2e
  RSSI: 0
  Connectable: false
  Advertised Services: none
  Manufacturer Data: none
  Service Data: none
  GATT Services: 6

[1] Service 1234567812345678123456789abcdef0
  [1.1] Characteristic 1234567812345678123456789abcdef1
      properties: Read
  [1.2] Characteristic 1234567812345678123456789abcdef2
      properties: Write
  [1.3] Characteristic 1234567812345678123456789abcdef3
      properties: Notify
  [1.4] Characteristic 1234567812345678123456789abcdef4
      properties: Read, Write

[2] Service 180a
  [2.1] Characteristic 2a24
      properties: Read
  [2.2] Characteristic 2a25
      properties: Read
  [2.3] Characteristic 2a26
      properties: Read
  [2.4] Characteristic 2a27
      properties: Read
  [2.5] Characteristic 2a29
      properties: Read

[3] Service 180d
  [3.1] Characteristic 2a37
      properties: Read, Notify

[4] Service 180f
  [4.1] Characteristic 2a19
      properties: Read, Notify

[5] Service 181a
  [5.1] Characteristic 2a6e
      properties: Read, Notify
  [5.2] Characteristic 2a6f
      properties: Read, Notify

[6] Service 6e400001b5a3f393e0a9e50e24dcca9e
  [6.1] Characteristic 6e400002b5a3f393e0a9e50e24dcca9e
      properties: Write
  [6.2] Characteristic 6e400003b5a3f393e0a9e50e24dcca9e
      properties: Notify
```