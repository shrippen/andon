package sources

// SetBases points the public-API sources at a test server; the returned
// func restores them.
func SetBases(base string) func() {
	saved := []string{nagerBase, jokeBase, coingeckoBase, yahooBase, xkcdBase, nasaBase, flightsBase, transitBase, rdapBase, ownIPURL}
	nagerBase, jokeBase, coingeckoBase, yahooBase, xkcdBase = base, base, base, base, base
	nasaBase, flightsBase, transitBase, rdapBase, ownIPURL = base, base, base, base, base+"/ip"
	return func() {
		nagerBase, jokeBase, coingeckoBase, yahooBase, xkcdBase = saved[0], saved[1], saved[2], saved[3], saved[4]
		nasaBase, flightsBase, transitBase, rdapBase, ownIPURL = saved[5], saved[6], saved[7], saved[8], saved[9]
	}
}

// SetWeatherURL points the weather source at a test server.
func SetWeatherURL(u string) func() {
	saved := openMeteoURL
	openMeteoURL = u
	return func() { openMeteoURL = saved }
}

// SetFrankfurter points the rates source at a test server.
func SetFrankfurter(u string) func() {
	saved := frankfurterBase
	frankfurterBase = u
	return func() { frankfurterBase = saved }
}

// SetPublicIPv6 points the IPv6 lookup at a test server.
func SetPublicIPv6(u string) func() {
	saved := publicIPv6URL
	publicIPv6URL = u
	return func() { publicIPv6URL = saved }
}
