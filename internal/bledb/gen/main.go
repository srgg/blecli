// Package main generates BLE database code from Nordic Semiconductor's bluetooth-numbers-database.
//
// This tool downloads BLE service, characteristic, descriptor, and vendor data from
// Nordic's GitHub repository and generates a lookup table in bledb_gen.go.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	cacheDir          = "../../.tmp/bledb-cache"
	outFile           = "../../internal/bledb/bledb_generated.go"
	serviceURL        = "https://raw.githubusercontent.com/NordicSemiconductor/bluetooth-numbers-database/master/v1/service_uuids.json"
	characteristicURL = "https://raw.githubusercontent.com/NordicSemiconductor/bluetooth-numbers-database/master/v1/characteristic_uuids.json"
	descriptorURL     = "https://raw.githubusercontent.com/NordicSemiconductor/bluetooth-numbers-database/master/v1/descriptor_uuids.json"
	vendorURL         = "https://raw.githubusercontent.com/NordicSemiconductor/bluetooth-numbers-database/master/v1/company_ids.json"

	bsigServiceURL        = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/service_uuids.yaml"
	bsigSdoURL            = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/sdo_uuids.yaml"
	bsigCharacteristicURL = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/characteristic_uuids.yaml"
	bsigDescriptorURL     = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/descriptors.yaml"
	bsigDeclarationURL    = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/declarations.yaml"
	bsigVendorURL         = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/company_identifiers/company_identifiers.yaml"
	bsigUnitURL           = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/units.yaml"
	bsigMemberUUIDsURL    = "https://bitbucket.org/bluetooth-SIG/public/raw/main/assigned_numbers/uuids/member_uuids.yaml"

	bleakURL = "https://raw.githubusercontent.com/hbldh/bleak/refs/heads/develop/bleak/uuids.py"
)

//go:embed bledb.go.tmpl
var codeTemplate string

// rawEntry represents a single BLE database entry before processing.
type rawEntry struct {
	UUID string
	Name string
	Type BLEType
}

// templateData holds the data for the code generation template.
type templateData struct {
	Timestamp             string
	ServiceURL            string
	CharacteristicURL     string
	DescriptorURL         string
	VendorURL             string
	BleakURL              string
	ServiceEntries        []templateEntry
	CharacteristicEntries []templateEntry
	DescriptorEntries     []templateEntry
	VendorEntries         []templateEntry
	UnitEntries           []templateEntry
	BleakEntries          []templateEntry
}

// templateEntry represents a UUID entry in the template.
type templateEntry struct {
	UUID string
	Name string
}

// BLEType represents the category of a BLE UUID.
type BLEType string

const (
	Service        BLEType = "Service"
	Characteristic BLEType = "Characteristic"
	Descriptor     BLEType = "Descriptor"
	Vendor         BLEType = "Vendor"
	Unit           BLEType = "Unit"
	Other          BLEType = "Other"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

// run executes the main generation logic.
func run() error {
	fmt.Println("Generating BLE database...")

	servicesPath, err := ensureCached("services.json", serviceURL)
	if err != nil {
		return err
	}
	services, err := parseJSONArray(servicesPath, Service)
	if err != nil {
		return err
	}

	characteristicsPath, err := ensureCached("characteristics.json", characteristicURL)
	if err != nil {
		return err
	}
	characteristics, err := parseJSONArray(characteristicsPath, Characteristic)
	if err != nil {
		return err
	}

	descriptorsPath, err := ensureCached("descriptors.json", descriptorURL)
	if err != nil {
		return err
	}
	descriptors, err := parseJSONArray(descriptorsPath, Descriptor)
	if err != nil {
		return err
	}

	vendorsPath, err := ensureCached("vendors.json", vendorURL)
	if err != nil {
		return err
	}
	vendors, err := parseJSONArray(vendorsPath, Vendor)
	if err != nil {
		return err
	}

	// -- Merge Bluetooth SIG data

	// BSIG Services
	bsigServicesPath, err := ensureCached("service_uuids.yaml", bsigServiceURL)
	if err != nil {
		return err
	}

	bsigServices, err := parseBluetoothSIGYAML(bsigServicesPath, Service)
	if err != nil {
		return err
	}

	services = append(services, bsigServices...)

	bsigSDOPath, err := ensureCached("sdo_uuids.yaml", bsigSdoURL)
	if err != nil {
		return err
	}

	bsigSDOs, err := parseBluetoothSIGYAML(bsigSDOPath, Service)
	if err != nil {
		return err
	}

	services = append(services, bsigSDOs...)

	// BSIG Characteristics
	bsigCharacteristicsPath, err := ensureCached("characteristic_uuids.yaml", bsigCharacteristicURL)
	if err != nil {
		return err
	}
	bsigCharacteristics, err := parseBluetoothSIGYAML(bsigCharacteristicsPath, Characteristic)
	if err != nil {
		return err
	}

	characteristics = append(characteristics, bsigCharacteristics...)

	// BSIG Descriptors
	bsigDescriptorsPath, err := ensureCached("descriptors.yaml", bsigDescriptorURL)
	if err != nil {
		return err
	}
	bsigDescriptors, err := parseBluetoothSIGYAML(bsigDescriptorsPath, Descriptor)
	if err != nil {
		return err
	}

	descriptors = append(descriptors, bsigDescriptors...)

	// BSIG Declarations
	bsigDeclarationsPath, err := ensureCached("declarations.yaml", bsigDeclarationURL)
	if err != nil {
		return err
	}
	bsigDeclarations, err := parseBluetoothSIGYAML(bsigDeclarationsPath, Descriptor)
	if err != nil {
		return err
	}

	descriptors = append(descriptors, bsigDeclarations...)

	// BSIG Vendors
	bsigVendorsPath, err := ensureCached("company_identifiers.yaml", bsigVendorURL)
	if err != nil {
		return err
	}

	bsigVendors, err := parseBluetoothSIGYAML(bsigVendorsPath, Vendor)
	if err != nil {
		return err
	}

	vendors = append(vendors, bsigVendors...)

	// BSIG Units
	bsigUnitsPath, err := ensureCached("units.yaml", bsigUnitURL)
	if err != nil {
		return err
	}
	bsigUnits, err := parseBluetoothSIGYAML(bsigUnitsPath, Unit)
	if err != nil {
		return err
	}

	// -- Last Hope Bleak unsorted UUIDs
	bleakPath, err := ensureCached("bleak_uuids.py", bleakURL)
	if err != nil {
		return err
	}

	bleakEntries, err := parseBleakUUIDs(bleakPath)
	if err != nil {
		return err
	}

	// -- Merge BSIG Member UUIDs with Bleak UUIDS as both are lost hope lookup
	bsigMemberUUIDsPath, err := ensureCached("member_uuids.yaml", bsigMemberUUIDsURL)
	if err != nil {
		return err
	}

	bsigMemberUUIDs, err := parseBluetoothSIGYAML(bsigMemberUUIDsPath, Other)
	if err != nil {
		return err
	}

	bleakEntries = append(bleakEntries, bsigMemberUUIDs...)

	timestamp := time.Now().UTC().Format(time.RFC3339)

	f, err := os.Create(outFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	if err := writeGeneratedFile(f, services, characteristics, descriptors, vendors, bsigUnits, bleakEntries, timestamp); err != nil {
		return fmt.Errorf("failed to write generated file: %w", err)
	}
	fmt.Println("Generated", outFile)
	return nil
}

// ensureCached downloads a file from the given URL if it doesn't exist in the cache.
// Returns the path to the cached file.
func ensureCached(filename, url string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	path := filepath.Join(cacheDir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("Downloading", filename)
		resp, err := http.Get(url)
		if err != nil {
			return "", fmt.Errorf("failed to download %s: %w", filename, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to download %s: status %d", filename, resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body for %s: %w", filename, err)
		}

		if err := os.WriteFile(path, data, 0644); err != nil {
			return "", fmt.Errorf("failed to write cache file %s: %w", filename, err)
		}
	} else if err != nil {
		return "", fmt.Errorf("failed to check cache file %s: %w", filename, err)
	} else {
		fmt.Println("Using cached file", filename)
	}
	return path, nil
}

// parseJSONArray parses a JSON file containing BLE database entries.
// The bleType parameter specifies the category of entries in the file.
func parseJSONArray(path string, bleType BLEType) ([]rawEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cached file %s: %w", path, err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, fmt.Errorf("failed to parse JSON array %s: %w", path, err)
	}

	entries := make([]rawEntry, 0, len(arr))
	for _, v := range arr {
		uuid := ""
		name := ""

		// For vendors, JSON uses "id" instead of "uuid"
		if bleType == Vendor {
			if id, ok := v["id"]; ok {
				switch id := id.(type) {
				case float64:
					uuid = fmt.Sprintf("%d", int(id))
				case string:
					uuid = id
				default:
					uuid = fmt.Sprintf("%v", id)
				}
			}
			if n, ok := v["name"].(string); ok {
				name = n
			}
		} else {
			if u, ok := v["uuid"].(string); ok {
				uuid = u
			}
			if n, ok := v["name"].(string); ok {
				name = n
			}
		}

		if uuid != "" && name != "" {
			entries = append(entries, rawEntry{
				UUID: uuid,
				Name: name,
				Type: bleType,
			})
		}
	}

	return entries, nil
}

// parseBluetoothSIGYAML parses a Bluetooth SIG YAML file and returns a single consolidated slice of rawEntry.
// Vendor entries come from "company_identifiers" (value + name) and are always Vendor type.
// Other BLE UUIDs come from "uuids" section (uuid + name) and get the caller-specified bleType.
// Fails if neither section is present or contains no valid entries.
func parseBluetoothSIGYAML(path string, bleType BLEType) ([]rawEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read SIG YAML file %s: %w", path, err)
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse SIG YAML %s: %w", path, err)
	}

	entries := make([]rawEntry, 0)

	// --- Parse Vendor entries ---
	if companiesRaw, ok := root["company_identifiers"]; ok {
		companies, ok := companiesRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid company_identifiers format in %s", path)
		}

		for _, c := range companies {
			compMap, ok := c.(map[string]interface{})
			if !ok {
				continue
			}

			uuid := ""
			if v, ok := compMap["value"]; ok {
				switch v := v.(type) {
				case int:
					uuid = fmt.Sprintf("%d", v)
				case float64:
					uuid = fmt.Sprintf("%d", int(v))
				case string:
					uuid = v
				default:
					uuid = fmt.Sprintf("%v", v)
				}
			}

			name := ""
			if n, ok := compMap["name"].(string); ok {
				name = n
			}

			if uuid != "" && name != "" {
				entries = append(entries, rawEntry{
					UUID: uuid,
					Name: name,
					Type: Vendor, // always Vendor
				})
			}
		}
	}

	// --- Parse Non-Vendor BLE UUIDs ---
	if uuidsRaw, ok := root["uuids"]; ok {
		uuids, ok := uuidsRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid uuids format in %s", path)
		}

		for _, u := range uuids {
			uuidMap, ok := u.(map[string]interface{})
			if !ok {
				continue
			}

			uuid := ""
			if v, ok := uuidMap["uuid"]; ok {
				switch v := v.(type) {
				case int:
					uuid = fmt.Sprintf("%d", v)
				case float64:
					uuid = fmt.Sprintf("%d", int(v))
				case string:
					uuid = v
				default:
					uuid = fmt.Sprintf("%v", v)
				}
			}

			name := ""
			if n, ok := uuidMap["name"].(string); ok {
				name = n
			}

			if uuid != "" && name != "" {
				entries = append(entries, rawEntry{
					UUID: uuid,
					Name: name,
					Type: bleType, // caller-specified type
				})
			}
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("SIG YAML file %s contains no valid company_identifiers or uuids", path)
	}

	return entries, nil
}

// parseBleakUUIDs extracts UUID/name pairs from Bleak's uuids.py file.
func parseBleakUUIDs(path string) ([]rawEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read Bleak file %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	var entries []rawEntry

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Match lines like:  "9fa480e0-4967-4542-9390-d343dc5d04ae": "Apple Nearby Service",
		if strings.HasPrefix(line, "\"") && strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			uuid := strings.Trim(parts[0], "\" ")
			name := strings.Trim(parts[1], " \",")
			if uuid != "" && name != "" {
				entries = append(entries, rawEntry{
					UUID: uuid,
					Name: name,
					Type: Other,
				})
			}
		}
	}
	return entries, nil
}

// writeGeneratedFile writes the BLE database to a Go source file using a template.
func writeGeneratedFile(f *os.File, services, characteristics, descriptors, vendors, units, bleakEntries []rawEntry, timestamp string) error {
	tmpl, err := template.New("bledb").Parse(codeTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Convert entries to template format by type
	convertEntries := func(entries []rawEntry, bleType BLEType) []templateEntry {
		// Normalize UUID to ble.UUID format (lowercase, no dashes/braces)
		normalizeUUID := func(uuid string) string {
			u := strings.ToLower(uuid)
			u = strings.ReplaceAll(u, "-", "")
			u = strings.ReplaceAll(u, "{", "")
			u = strings.ReplaceAll(u, "}", "")
			return u
		}

		// First, normalize and collect entries
		normalized := make([]templateEntry, 0, len(entries))
		for _, e := range entries {
			if e.UUID == "" || e.Name == "" {
				continue
			}
			normalized = append(normalized, templateEntry{
				UUID: normalizeUUID(e.UUID),
				Name: e.Name,
			})
		}

		// Sort by UUID
		sort.Slice(normalized, func(i, j int) bool {
			return normalized[i].UUID < normalized[j].UUID
		})

		// Detect and remove duplicates (first write wins)
		result := make([]templateEntry, 0, len(normalized))
		seen := make(map[string]string)
		for _, e := range normalized {
			if existingName, exists := seen[e.UUID]; exists {
				// Only warn if the data is different (same UUID, different name)
				if existingName != e.Name {
					fmt.Fprintf(os.Stderr, "WARNING: Duplicate UUID %q in %s (keeping %q, skipping %q)\n",
						e.UUID, bleType, existingName, e.Name)
				}
				// Skip duplicate (whether identical or different)
				continue
			}
			seen[e.UUID] = e.Name
			result = append(result, e)
		}

		return result
	}

	data := templateData{
		Timestamp:             timestamp,
		ServiceURL:            serviceURL,
		CharacteristicURL:     characteristicURL,
		DescriptorURL:         descriptorURL,
		VendorURL:             vendorURL,
		BleakURL:              bleakURL,
		ServiceEntries:        convertEntries(services, Service),
		CharacteristicEntries: convertEntries(characteristics, Characteristic),
		DescriptorEntries:     convertEntries(descriptors, Descriptor),
		VendorEntries:         convertEntries(vendors, Vendor),
		UnitEntries:           convertEntries(units, Unit),
		BleakEntries:          convertEntries(bleakEntries, Other),
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}
