package wifi

import (
	"fmt"
	"sort"
)

type RFChannelInfo struct {
	Channel         int      `json:"channel"`
	Band            string   `json:"band"` // "2.4GHz", "5GHz", "6GHz"
	FrequencyMHz    int      `json:"frequency_mhz"`
	BSSIDCount      int      `json:"bssid_count"`
	SSIDs           []string `json:"ssids"`
	AvgRSSI         int      `json:"avg_rssi"`
	CongestionLevel string   `json:"congestion_level"` // "Clean", "Low", "Moderate", "Congested"
	Score           float64  `json:"score"`            // 0.0 - 100.0 (100 = cleanest)
	IsNonOverlapping bool    `json:"is_non_overlapping"`
}

type SpectrumReport struct {
	Channels24GHz       []RFChannelInfo `json:"channels_24ghz"`
	Channels5GHz        []RFChannelInfo `json:"channels_5ghz"`
	OptimalChannel24GHz int             `json:"optimal_channel_24ghz"`
	OptimalChannel5GHz  int             `json:"optimal_channel_5ghz"`
	TotalNetworks       int             `json:"total_networks"`
	Recommendations     []string        `json:"recommendations"`
}

// GenerateSpectrumReport analyzes observed Wi-Fi networks and builds RF channel heatmaps.
func GenerateSpectrumReport(hotspots []*AccessPoint) SpectrumReport {
	// Initialize standard 2.4 GHz channels (1 - 13)
	chanMap24 := make(map[int]*RFChannelInfo)
	for ch := 1; ch <= 13; ch++ {
		freq := 2407 + (ch * 5)
		if ch == 14 {
			freq = 2484
		}
		chanMap24[ch] = &RFChannelInfo{
			Channel:          ch,
			Band:             "2.4GHz",
			FrequencyMHz:     freq,
			SSIDs:            make([]string, 0),
			AvgRSSI:          -100,
			CongestionLevel:  "Clean",
			Score:            100.0,
			IsNonOverlapping: ch == 1 || ch == 6 || ch == 11,
		}
	}

	// Initialize standard 5 GHz UNII channels (36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165)
	std5GChans := []int{36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165}
	chanMap5G := make(map[int]*RFChannelInfo)
	for _, ch := range std5GChans {
		freq := 5000 + (ch * 5)
		chanMap5G[ch] = &RFChannelInfo{
			Channel:          ch,
			Band:             "5GHz",
			FrequencyMHz:     freq,
			SSIDs:            make([]string, 0),
			AvgRSSI:          -100,
			CongestionLevel:  "Clean",
			Score:            100.0,
			IsNonOverlapping: true,
		}
	}

	// Populate observed hotspots
	rssiSum24 := make(map[int]int)
	rssiSum5G := make(map[int]int)

	for _, h := range hotspots {
		ch := int(h.Channel)
		if ch >= 1 && ch <= 14 {
			info, ok := chanMap24[ch]
			if !ok {
				continue
			}
			info.BSSIDCount++
			if h.SSID != "" && h.SSID != "<hidden>" {
				info.SSIDs = append(info.SSIDs, h.SSID)
			}
			rssiSum24[ch] += int(h.RSSI)
		} else if ch >= 36 && ch <= 165 {
			info, ok := chanMap5G[ch]
			if !ok {
				// dynamically add non-standard channel if observed
				info = &RFChannelInfo{
					Channel:          ch,
					Band:             "5GHz",
					FrequencyMHz:     5000 + (ch * 5),
					SSIDs:            make([]string, 0),
					IsNonOverlapping: true,
				}
				chanMap5G[ch] = info
			}
			info.BSSIDCount++
			if h.SSID != "" && h.SSID != "<hidden>" {
				info.SSIDs = append(info.SSIDs, h.SSID)
			}
			rssiSum5G[ch] += int(h.RSSI)
		}
	}

	// Calculate scores & congestion levels for 2.4 GHz
	var list24 []RFChannelInfo
	for ch := 1; ch <= 13; ch++ {
		info := chanMap24[ch]
		if info.BSSIDCount > 0 {
			info.AvgRSSI = rssiSum24[ch] / info.BSSIDCount
		}

		// Co-channel & adjacent channel interference penalty
		penalty := float64(info.BSSIDCount) * 22.0
		// Neighbor overlap penalty for 2.4GHz
		for offset := -2; offset <= 2; offset++ {
			if offset == 0 {
				continue
			}
			neighbor := ch + offset
			if nInfo, exists := chanMap24[neighbor]; exists {
				penalty += float64(nInfo.BSSIDCount) * 12.0
			}
		}
		if !info.IsNonOverlapping {
			penalty += 15.0 // Non-standard channels 2,3,4,5,7,8,9,10 suffer overlap
		}

		score := 100.0 - penalty
		if score < 5.0 {
			score = 5.0
		}
		info.Score = score

		if info.BSSIDCount == 0 && score >= 80.0 {
			info.CongestionLevel = "Clean"
		} else if info.BSSIDCount <= 1 && score >= 60.0 {
			info.CongestionLevel = "Low"
		} else if info.BSSIDCount <= 3 {
			info.CongestionLevel = "Moderate"
		} else {
			info.CongestionLevel = "Congested"
		}
		list24 = append(list24, *info)
	}

	// Calculate scores & congestion levels for 5 GHz
	var list5G []RFChannelInfo
	for _, ch := range std5GChans {
		info := chanMap5G[ch]
		if info.BSSIDCount > 0 {
			info.AvgRSSI = rssiSum5G[ch] / info.BSSIDCount
		}

		penalty := float64(info.BSSIDCount) * 20.0
		score := 100.0 - penalty
		if score < 10.0 {
			score = 10.0
		}
		info.Score = score

		if info.BSSIDCount == 0 {
			info.CongestionLevel = "Clean"
		} else if info.BSSIDCount == 1 {
			info.CongestionLevel = "Low"
		} else if info.BSSIDCount <= 3 {
			info.CongestionLevel = "Moderate"
		} else {
			info.CongestionLevel = "Congested"
		}
		list5G = append(list5G, *info)
	}

	// Find optimal 2.4 GHz (prefer non-overlapping 1, 6, 11)
	best24 := 1
	var bestScore24 float64 = -1
	for _, ch := range []int{1, 6, 11} {
		if chanMap24[ch].Score > bestScore24 {
			bestScore24 = chanMap24[ch].Score
			best24 = ch
		}
	}

	// Find optimal 5 GHz channel
	best5G := 36
	var bestScore5G float64 = -1
	for _, ch := range std5GChans {
		if chanMap5G[ch].Score > bestScore5G {
			bestScore5G = chanMap5G[ch].Score
			best5G = ch
		}
	}

	// Generate recommendations
	var recs []string
	recs = append(recs, fmt.Sprintf("Cleanest 2.4 GHz Channel: Channel %d (Score: %.0f/100)", best24, bestScore24))
	recs = append(recs, fmt.Sprintf("Cleanest 5 GHz Channel: Channel %d (Score: %.0f/100)", best5G, bestScore5G))
	if chanMap24[6].BSSIDCount > 2 {
		recs = append(recs, "Channel 6 has high co-channel density; consider switching to Channel 1 or 11.")
	}
	recs = append(recs, "Wi-Fi 6 (802.11ax) dual-band dongle supports 80 MHz channel width for 5 GHz.")

	sort.Slice(list24, func(i, j int) bool { return list24[i].Channel < list24[j].Channel })
	sort.Slice(list5G, func(i, j int) bool { return list5G[i].Channel < list5G[j].Channel })

	return SpectrumReport{
		Channels24GHz:       list24,
		Channels5GHz:        list5G,
		OptimalChannel24GHz: best24,
		OptimalChannel5GHz:  best5G,
		TotalNetworks:       len(hotspots),
		Recommendations:     recs,
	}
}
