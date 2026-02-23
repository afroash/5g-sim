// helpers.go — Shared utility functions for the gnb package.
package gnb

// encodePLMNBytes encodes a PLMN string into the 3-byte BCD format
// used throughout 3GPP specifications.
//
// Input: 5 or 6 digit string e.g. "00101" (MCC=001 MNC=01)
// Output: 3 bytes BCD encoded
//
// Ref: TS 24.008 §10.5.1.13
func encodePLMNBytes(plmn string) []byte {
	// Pad to 6 chars if 2-digit MNC (insert 'f' as MNC digit 3)
	if len(plmn) == 5 {
		plmn = plmn[:3] + "f" + plmn[3:]
	}

	digit := func(i int) byte {
		if plmn[i] == 'f' || plmn[i] == 'F' {
			return 0xF
		}
		return plmn[i] - '0'
	}

	return []byte{
		(digit(1) << 4) | digit(0), // MCC digit 2, MCC digit 1
		(digit(3) << 4) | digit(2), // MNC digit 3 (or F), MCC digit 3
		(digit(5) << 4) | digit(4), // MNC digit 2, MNC digit 1
	}
}
