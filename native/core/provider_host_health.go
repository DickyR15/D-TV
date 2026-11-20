package core

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

type providerEndpointHealth struct {
	AverageLatency time.Duration
	Successes uint64
	Failures uint64
	ConsecutiveFailure uint32
	CooldownUntil time.Time
	LastSuccess time.Time
}

func (d *Downloader) orderProviderEndpoints(source, role string, candidates []string) []string {
	if d == nil || len(candidates) < 2 { return append([]string(nil), candidates...) }
	type ranked struct { value string; index int; health providerEndpointHealth; class int }
	rows := make([]ranked, 0, len(candidates))
	now := time.Now()
	d.providerHealthMu.Lock()
	for index, candidate := range candidates {
		host := providerEndpointHost(candidate)
		key := canonicalProviderSource(source) + "|" + role + "|" + host
		state := d.providerHealth[key]
		class := 1
		if !state.CooldownUntil.IsZero() && now.Before(state.CooldownUntil) { class = 2
		} else if state.Successes > 0 { class = 0 }
		rows = append(rows, ranked{candidate,index,state,class})
	}
	d.providerHealthMu.Unlock()
	sort.SliceStable(rows, func(i,j int) bool {
		a,b:=rows[i],rows[j]
		if a.class!=b.class { return a.class<b.class }
		if a.class==0 {
			aa:=a.health.AverageLatency+time.Duration(a.health.ConsecutiveFailure)*3*time.Second
			bb:=b.health.AverageLatency+time.Duration(b.health.ConsecutiveFailure)*3*time.Second
			if aa!=bb { return aa<bb }
			if !a.health.LastSuccess.Equal(b.health.LastSuccess) { return a.health.LastSuccess.After(b.health.LastSuccess) }
		}
		return a.index<b.index
	})
	out:=make([]string,0,len(rows));for _,row:=range rows {out=append(out,row.value)}
	return out
}

func (d *Downloader) orderProviderHostNames(source, role string, hosts []string) []string {
	urls:=make([]string,0,len(hosts));original:=make(map[string]string,len(hosts))
	for _,host:=range hosts { host=strings.TrimSpace(host);if host==""{continue};address:="https://"+host;urls=append(urls,address);original[address]=host }
	ordered:=d.orderProviderEndpoints(source,role,urls);out:=make([]string,0,len(ordered))
	for _,address:=range ordered {if host:=original[address];host!=""{out=append(out,host)}}
	return out
}

func (d *Downloader) recordProviderEndpoint(source, role, rawURL string, elapsed time.Duration, requestErr error) {
	if d==nil {return}
	host:=providerEndpointHost(rawURL);if host==""||source=="" {return}
	key:=canonicalProviderSource(source)+"|"+role+"|"+host
	now:=time.Now();if elapsed<0 {elapsed=0}
	d.providerHealthMu.Lock()
	if d.providerHealth==nil {d.providerHealth=make(map[string]providerEndpointHealth)}
	state:=d.providerHealth[key]
	if requestErr==nil {
		if state.Successes==0||state.AverageLatency<=0 {state.AverageLatency=elapsed} else {state.AverageLatency=(state.AverageLatency*3+elapsed)/4}
		state.Successes++;state.ConsecutiveFailure=0;state.CooldownUntil=time.Time{};state.LastSuccess=now
	} else {
		state.Failures++;state.ConsecutiveFailure++
		backoff:=10*time.Second
		for n:=uint32(1);n<state.ConsecutiveFailure&&backoff<5*time.Minute;n++ {backoff*=2}
		if backoff>5*time.Minute {backoff=5*time.Minute}
		state.CooldownUntil=now.Add(backoff)
	}
	d.providerHealth[key]=state
	d.providerHealthMu.Unlock()
}

func providerEndpointHost(raw string) string {
	parsed,err:=url.Parse(strings.TrimSpace(raw));if err!=nil{return ""}
	return strings.ToLower(parsed.Hostname())
}

func containsString(values []string, wanted string) bool {
	for _,value:=range values {if value==wanted{return true}}
	return false
}
