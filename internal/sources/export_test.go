package sources

import "time"

// SetBases points the public-API sources at a test server; the returned
// func restores them.
func SetBases(base string) func() {
	saved := []string{nagerBase, jokeBase, coingeckoBase, yahooBase, xkcdBase, nasaBase, flightsBase, transitBase, rdapBase, ownIPURL}
	nagerBase, jokeBase, coingeckoBase, yahooBase, xkcdBase = base, base, base, base, base
	nasaBase, flightsBase, transitBase, rdapBase, ownIPURL = base, base, base, base, base+"/ip"
	trends, liga := githubSearchBase, openLigaBase
	githubSearchBase, openLigaBase = base, base
	servers := rdapServers
	rdapServers = map[string]string{}
	for tld := range servers {
		rdapServers[tld] = base
	}
	return func() {
		rdapServers = servers
		githubSearchBase, openLigaBase = trends, liga
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

// FeedURL exposes how a calendar address becomes a fetchable one.
var FeedURL = feedURL

// SetClock fixes the time the GitHub download memo sees.
func SetClock(at func() time.Time) func() {
	saved := clockNow
	clockNow = at
	return func() { clockNow = saved }
}

// DownloadsEvery is downloadsEvery for tests.
var DownloadsEvery = downloadsEvery

// SetRDAPServer points the RDAP lookup of one top-level domain at a test
// server.
func SetRDAPServer(tld, u string) func() {
	saved, had := rdapServers[tld]
	rdapServers[tld] = u
	return func() {
		if had {
			rdapServers[tld] = saved
			return
		}
		delete(rdapServers, tld)
	}
}

// SetTrackBudget bounds the single-track reads of one Dawarich fetch.
func SetTrackBudget(d time.Duration) func() {
	saved := trackBudget
	trackBudget = d
	return func() { trackBudget = saved }
}

// SetReadingBases points the reading sites and Twitch's token endpoint at
// base and returns the undo.
func SetReadingBases(base string) func() {
	saved := []string{hnBase, lobstersBase, redditBase, youtubeBase, twitchIDBase}
	hnBase, lobstersBase, redditBase, youtubeBase, twitchIDBase = base, base, base, base, base
	return func() {
		hnBase, lobstersBase, redditBase, youtubeBase, twitchIDBase = saved[0], saved[1], saved[2], saved[3], saved[4]
	}
}
