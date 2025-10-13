/*
 * ESP32-S3 BLE Test Peripheral Device
 *
 * This firmware implements a comprehensive BLE peripheral with multiple services
 * and characteristics to simulate various BLE device features for testing and debugging.
 *
 * Features:
 * - Device Information Service
 * - Battery Service
 * - Heart Rate Service (simulated)
 * - Environmental Sensing Service (temperature, humidity)
 * - Custom Test Service with Read/Write/Notify characteristics
 * - Serial-like UART Service
 */

#include <Arduino.h>
#include <BLEDevice.h>
#include <BLEServer.h>
#include <BLEUtils.h>
#include <BLE2902.h>

// Device Configuration
#define DEVICE_NAME "ESP32-S3-BLIM-TestPeripheral"
#define MANUFACTURER_NAME "BLIMCo"
#define MODEL_NUMBER "ESP32-S3-DevKit-1"
#define SERIAL_NUMBER "TEST-001"
#define FIRMWARE_VERSION "1.0.0"
#define HARDWARE_VERSION "1.0"

// Service UUIDs (Standard BLE Services)
#define SERVICE_DEVICE_INFO         "180A"
#define SERVICE_BATTERY             "180F"
#define SERVICE_HEART_RATE          "180D"
#define SERVICE_ENV_SENSING         "181A"
#define SERVICE_UART                "6E400001-B5A3-F393-E0A9-E50E24DCCA9E"  // Nordic UART Service

// Custom Test Service
#define SERVICE_CUSTOM_TEST         "12345678-1234-5678-1234-56789abcdef0"

// Device Information Characteristics
#define CHAR_MANUFACTURER_NAME      "2A29"
#define CHAR_MODEL_NUMBER          "2A24"
#define CHAR_SERIAL_NUMBER         "2A25"
#define CHAR_FIRMWARE_REVISION     "2A26"
#define CHAR_HARDWARE_REVISION     "2A27"

// Battery Service Characteristics
#define CHAR_BATTERY_LEVEL         "2A19"

// Heart Rate Service Characteristics
#define CHAR_HEART_RATE            "2A37"

// Environmental Sensing Characteristics
#define CHAR_TEMPERATURE           "2A6E"
#define CHAR_HUMIDITY              "2A6F"

// UART Service Characteristics
#define CHAR_UART_TX               "6E400003-B5A3-F393-E0A9-E50E24DCCA9E"
#define CHAR_UART_RX               "6E400002-B5A3-F393-E0A9-E50E24DCCA9E"

// Custom Test Service Characteristics
#define CHAR_TEST_READ             "12345678-1234-5678-1234-56789abcdef1"
#define CHAR_TEST_WRITE            "12345678-1234-5678-1234-56789abcdef2"
#define CHAR_TEST_NOTIFY           "12345678-1234-5678-1234-56789abcdef3"
#define CHAR_TEST_READWRITE        "12345678-1234-5678-1234-56789abcdef4"

// Global objects
BLEServer *pServer = nullptr;
BLECharacteristic *pBatteryCharacteristic = nullptr;
BLECharacteristic *pHeartRateCharacteristic = nullptr;
BLECharacteristic *pTemperatureCharacteristic = nullptr;
BLECharacteristic *pHumidityCharacteristic = nullptr;
BLECharacteristic *pTestNotifyCharacteristic = nullptr;
BLECharacteristic *pUartTxCharacteristic = nullptr;
BLECharacteristic *pUartRxCharacteristic = nullptr;

// State variables
bool deviceConnected = false;
bool oldDeviceConnected = false;
uint8_t batteryLevel = 100;
uint8_t heartRate = 72;
int16_t temperature = 2200;  // 22.00°C in hundredths
uint16_t humidity = 5500;    // 55.00% in hundredths
uint32_t notifyCounter = 0;

// Server Callbacks
class MyServerCallbacks: public BLEServerCallbacks {
    void onConnect(BLEServer* pServer) {
        deviceConnected = true;
        Serial.println("Client connected");
    };

    void onDisconnect(BLEServer* pServer) {
        deviceConnected = false;
        Serial.println("Client disconnected");
    }
};

// UART RX Callback
class UartRxCallbacks: public BLECharacteristicCallbacks {
    void onWrite(BLECharacteristic *pCharacteristic) {
        std::string rxValue = pCharacteristic->getValue();

        if (rxValue.length() > 0) {
            Serial.print("UART RX: ");
            for (int i = 0; i < rxValue.length(); i++) {
                Serial.print(rxValue[i]);
            }
            Serial.println();

            // Echo back with prefix
            std::string response = "Echo: " + rxValue;
            pUartTxCharacteristic->setValue(response);
            pUartTxCharacteristic->notify();
        }
    }
};

// Custom Test Write Callback
class TestWriteCallbacks: public BLECharacteristicCallbacks {
    void onWrite(BLECharacteristic *pCharacteristic) {
        std::string value = pCharacteristic->getValue();

        if (value.length() > 0) {
            Serial.print("Test Write received: ");
            for (int i = 0; i < value.length(); i++) {
                Serial.print(value[i]);
            }
            Serial.println();
        }
    }
};

void setupDeviceInformationService() {
    BLEService *pService = pServer->createService(SERVICE_DEVICE_INFO);

    BLECharacteristic *pManufacturer = pService->createCharacteristic(
        CHAR_MANUFACTURER_NAME,
        BLECharacteristic::PROPERTY_READ
    );
    pManufacturer->setValue(MANUFACTURER_NAME);

    BLECharacteristic *pModel = pService->createCharacteristic(
        CHAR_MODEL_NUMBER,
        BLECharacteristic::PROPERTY_READ
    );
    pModel->setValue(MODEL_NUMBER);

    BLECharacteristic *pSerial = pService->createCharacteristic(
        CHAR_SERIAL_NUMBER,
        BLECharacteristic::PROPERTY_READ
    );
    pSerial->setValue(SERIAL_NUMBER);

    BLECharacteristic *pFirmware = pService->createCharacteristic(
        CHAR_FIRMWARE_REVISION,
        BLECharacteristic::PROPERTY_READ
    );
    pFirmware->setValue(FIRMWARE_VERSION);

    BLECharacteristic *pHardware = pService->createCharacteristic(
        CHAR_HARDWARE_REVISION,
        BLECharacteristic::PROPERTY_READ
    );
    pHardware->setValue(HARDWARE_VERSION);

    pService->start();
    Serial.println("Device Information Service started");
}

void setupBatteryService() {
    BLEService *pService = pServer->createService(SERVICE_BATTERY);

    pBatteryCharacteristic = pService->createCharacteristic(
        CHAR_BATTERY_LEVEL,
        BLECharacteristic::PROPERTY_READ | BLECharacteristic::PROPERTY_NOTIFY
    );

    // Add CCCD (0x2902) for notifications
    pBatteryCharacteristic->addDescriptor(new BLE2902());

    // Add User Description (0x2901)
    BLEDescriptor *pBatteryDesc = new BLEDescriptor(BLEUUID((uint16_t)0x2901));
    pBatteryDesc->setValue("Device Battery Level");
    pBatteryCharacteristic->addDescriptor(pBatteryDesc);

    // Add Presentation Format (0x2904) for battery percentage
    BLEDescriptor *pBatteryFormat = new BLEDescriptor(BLEUUID((uint16_t)0x2904));
    uint8_t formatValue[7] = {
        0x04,        // Format: unsigned 8-bit integer
        0x00,        // Exponent: 0
        0xAD, 0x27,  // Unit: percentage (0x27AD)
        0x01,        // Namespace: Bluetooth SIG
        0x00, 0x00   // Description: 0
    };
    pBatteryFormat->setValue(formatValue, 7);
    pBatteryCharacteristic->addDescriptor(pBatteryFormat);

    pBatteryCharacteristic->setValue(&batteryLevel, 1);

    pService->start();
    Serial.println("Battery Service started");
}

void setupHeartRateService() {
    BLEService *pService = pServer->createService(SERVICE_HEART_RATE);

    pHeartRateCharacteristic = pService->createCharacteristic(
        CHAR_HEART_RATE,
        BLECharacteristic::PROPERTY_READ | BLECharacteristic::PROPERTY_NOTIFY
    );

    // Add CCCD (0x2902) for notifications
    pHeartRateCharacteristic->addDescriptor(new BLE2902());

    // Add User Description (0x2901)
    BLEDescriptor *pHRDesc = new BLEDescriptor(BLEUUID((uint16_t)0x2901));
    pHRDesc->setValue("Heart Rate Measurement");
    pHeartRateCharacteristic->addDescriptor(pHRDesc);

    // Heart rate measurement format: flags (1 byte) + heart rate (1 byte)
    uint8_t hrValue[2] = {0x00, heartRate};
    pHeartRateCharacteristic->setValue(hrValue, 2);

    pService->start();
    Serial.println("Heart Rate Service started");
}

void setupEnvironmentalSensingService() {
    BLEService *pService = pServer->createService(SERVICE_ENV_SENSING);

    // Temperature characteristic
    pTemperatureCharacteristic = pService->createCharacteristic(
        CHAR_TEMPERATURE,
        BLECharacteristic::PROPERTY_READ | BLECharacteristic::PROPERTY_NOTIFY
    );
    pTemperatureCharacteristic->addDescriptor(new BLE2902());

    // Add User Description (0x2901)
    BLEDescriptor *pTempDesc = new BLEDescriptor(BLEUUID((uint16_t)0x2901));
    pTempDesc->setValue("Ambient Temperature");
    pTemperatureCharacteristic->addDescriptor(pTempDesc);

    // Add Presentation Format (0x2904) for temperature in Celsius
    BLEDescriptor *pTempFormat = new BLEDescriptor(BLEUUID((uint16_t)0x2904));
    uint8_t tempFormatValue[7] = {
        0x0E,        // Format: signed 16-bit integer
        0xFE,        // Exponent: -2 (hundredths)
        0x2F, 0x27,  // Unit: degrees Celsius (0x272F)
        0x01,        // Namespace: Bluetooth SIG
        0x00, 0x00   // Description: 0
    };
    pTempFormat->setValue(tempFormatValue, 7);
    pTemperatureCharacteristic->addDescriptor(pTempFormat);

    pTemperatureCharacteristic->setValue((uint8_t*)&temperature, 2);

    // Humidity characteristic
    pHumidityCharacteristic = pService->createCharacteristic(
        CHAR_HUMIDITY,
        BLECharacteristic::PROPERTY_READ | BLECharacteristic::PROPERTY_NOTIFY
    );
    pHumidityCharacteristic->addDescriptor(new BLE2902());

    // Add User Description (0x2901)
    BLEDescriptor *pHumDesc = new BLEDescriptor(BLEUUID((uint16_t)0x2901));
    pHumDesc->setValue("Relative Humidity");
    pHumidityCharacteristic->addDescriptor(pHumDesc);

    // Add Presentation Format (0x2904) for humidity percentage
    BLEDescriptor *pHumFormat = new BLEDescriptor(BLEUUID((uint16_t)0x2904));
    uint8_t humFormatValue[7] = {
        0x06,        // Format: unsigned 16-bit integer
        0xFE,        // Exponent: -2 (hundredths)
        0xAD, 0x27,  // Unit: percentage (0x27AD)
        0x01,        // Namespace: Bluetooth SIG
        0x00, 0x00   // Description: 0
    };
    pHumFormat->setValue(humFormatValue, 7);
    pHumidityCharacteristic->addDescriptor(pHumFormat);

    pHumidityCharacteristic->setValue((uint8_t*)&humidity, 2);

    pService->start();
    Serial.println("Environmental Sensing Service started");
}

void setupUartService() {
    BLEService *pService = pServer->createService(SERVICE_UART);

    pUartTxCharacteristic = pService->createCharacteristic(
        CHAR_UART_TX,
        BLECharacteristic::PROPERTY_NOTIFY
    );
    pUartTxCharacteristic->addDescriptor(new BLE2902());

    pUartRxCharacteristic = pService->createCharacteristic(
        CHAR_UART_RX,
        BLECharacteristic::PROPERTY_WRITE | BLECharacteristic::PROPERTY_WRITE_NR
    );
    pUartRxCharacteristic->setCallbacks(new UartRxCallbacks());

    pService->start();
    Serial.println("UART Service started");
}

void setupCustomTestService() {
    BLEService *pService = pServer->createService(SERVICE_CUSTOM_TEST);

    // Read-only characteristic with multiple descriptors
    BLECharacteristic *pReadChar = pService->createCharacteristic(
        CHAR_TEST_READ,
        BLECharacteristic::PROPERTY_READ
    );
    pReadChar->setValue("ReadOnlyValue");

    // Add User Description (0x2901)
    BLEDescriptor *pReadDesc = new BLEDescriptor(BLEUUID((uint16_t)0x2901));
    pReadDesc->setValue("Test Read-Only Characteristic");
    pReadChar->addDescriptor(pReadDesc);

    // Add Characteristic Extended Properties Descriptor (0x2900)
    // Value: 0x0001 = Reliable Write enabled
    BLEDescriptor *pExtPropsDesc = new BLEDescriptor(BLEUUID((uint16_t)0x2900));
    uint8_t extPropsValue[2] = {0x01, 0x00};  // Little-endian: 0x0001
    pExtPropsDesc->setValue(extPropsValue, 2);
    pReadChar->addDescriptor(pExtPropsDesc);

    // Write-only characteristic
    BLECharacteristic *pWriteChar = pService->createCharacteristic(
        CHAR_TEST_WRITE,
        BLECharacteristic::PROPERTY_WRITE
    );
    pWriteChar->setCallbacks(new TestWriteCallbacks());

    // Notify characteristic
    pTestNotifyCharacteristic = pService->createCharacteristic(
        CHAR_TEST_NOTIFY,
        BLECharacteristic::PROPERTY_NOTIFY
    );
    pTestNotifyCharacteristic->addDescriptor(new BLE2902());

    // Read/Write characteristic
    BLECharacteristic *pReadWriteChar = pService->createCharacteristic(
        CHAR_TEST_READWRITE,
        BLECharacteristic::PROPERTY_READ | BLECharacteristic::PROPERTY_WRITE
    );
    pReadWriteChar->setValue("ReadWriteValue");
    pReadWriteChar->setCallbacks(new TestWriteCallbacks());

    pService->start();
    Serial.println("Custom Test Service started");
}

void setup() {
    Serial.begin(115200);
    delay(1000);

    Serial.println("\n=== ESP32-S3 BLE Test Peripheral ===");
    Serial.println("Manufacturer: " MANUFACTURER_NAME);
    Serial.println("Model: " MODEL_NUMBER);
    Serial.println("Firmware: " FIRMWARE_VERSION);
    Serial.println();

    // Initialize BLE
    BLEDevice::init(DEVICE_NAME);

    // Create BLE Server
    pServer = BLEDevice::createServer();
    pServer->setCallbacks(new MyServerCallbacks());

    // Setup all services
    setupDeviceInformationService();
    setupBatteryService();
    setupHeartRateService();
    setupEnvironmentalSensingService();
    setupUartService();
    setupCustomTestService();

    // Start advertising
    BLEAdvertising *pAdvertising = BLEDevice::getAdvertising();
    pAdvertising->addServiceUUID(SERVICE_DEVICE_INFO);
    pAdvertising->addServiceUUID(SERVICE_BATTERY);
    pAdvertising->addServiceUUID(SERVICE_HEART_RATE);
    pAdvertising->addServiceUUID(SERVICE_ENV_SENSING);
    pAdvertising->addServiceUUID(SERVICE_UART);
    pAdvertising->addServiceUUID(SERVICE_CUSTOM_TEST);
    pAdvertising->setScanResponse(true);
    pAdvertising->setMinPreferred(0x06);
    pAdvertising->setMinPreferred(0x12);
    BLEDevice::startAdvertising();

    Serial.println("BLE advertising started");
    Serial.println("Device name: " DEVICE_NAME);
    Serial.println("Ready for connections!");
    Serial.println();
}

void updateSensorValues() {
    // Simulate battery drain
    if (batteryLevel > 0 && millis() % 10000 < 100) {
        batteryLevel--;
        if (batteryLevel < 1) batteryLevel = 100;
    }

    // Simulate heart rate variation (60-100 bpm)
    heartRate = 72 + random(-12, 13);

    // Simulate temperature variation (20-25°C)
    temperature = 2200 + random(-200, 201);

    // Simulate humidity variation (45-65%)
    humidity = 5500 + random(-1000, 1001);

    // Increment notify counter
    notifyCounter++;
}

void sendNotifications() {
    if (!deviceConnected) return;

    // Battery level notification (every 5 seconds)
    if (millis() % 5000 < 100) {
        pBatteryCharacteristic->setValue(&batteryLevel, 1);
        pBatteryCharacteristic->notify();
        Serial.printf("Battery: %d%%\n", batteryLevel);
    }

    // Heart rate notification (every 1 second)
    if (millis() % 1000 < 100) {
        uint8_t hrValue[2] = {0x00, heartRate};
        pHeartRateCharacteristic->setValue(hrValue, 2);
        pHeartRateCharacteristic->notify();
        Serial.printf("Heart Rate: %d bpm\n", heartRate);
    }

    // Temperature notification (every 2 seconds)
    if (millis() % 2000 < 100) {
        pTemperatureCharacteristic->setValue((uint8_t*)&temperature, 2);
        pTemperatureCharacteristic->notify();
        Serial.printf("Temperature: %.2f°C\n", temperature / 100.0);
    }

    // Humidity notification (every 3 seconds)
    if (millis() % 3000 < 100) {
        pHumidityCharacteristic->setValue((uint8_t*)&humidity, 2);
        pHumidityCharacteristic->notify();
        Serial.printf("Humidity: %.2f%%\n", humidity / 100.0);
    }

    // Custom test notification (every 1 second)
    if (millis() % 1000 < 100) {
        char notifyMsg[32];
        snprintf(notifyMsg, sizeof(notifyMsg), "Counter: %lu", notifyCounter);
        pTestNotifyCharacteristic->setValue(notifyMsg);
        pTestNotifyCharacteristic->notify();
        Serial.printf("Test Notify: %s\n", notifyMsg);
    }
}

void loop() {
    updateSensorValues();
    sendNotifications();

    // Handle connection state changes
    if (!deviceConnected && oldDeviceConnected) {
        delay(500);
        pServer->startAdvertising();
        Serial.println("Restarted advertising");
        oldDeviceConnected = deviceConnected;
    }

    if (deviceConnected && !oldDeviceConnected) {
        oldDeviceConnected = deviceConnected;
    }

    delay(100);
}