package tmdb

import (
	"reflect"
	"testing"
)

func TestClassifyChannelProgramCategories(t *testing.T) {
	tests := []struct {
		name      string
		callSign  string
		affiliate string
		want      []string
	}{
		{name: "cnn hd", callSign: "CNNHD", want: []string{"News"}},
		{name: "fox news", callSign: "FNCHD", want: []string{"News"}},
		{name: "newsmax", callSign: "NEWSMXH", want: []string{"News"}},
		{name: "newsnation hd", callSign: "NEWSNTN", affiliate: "Independent", want: []string{"News"}},
		{name: "newsnation sd", callSign: "NWSNTSD", affiliate: "Independent", want: []string{"News"}},
		{name: "news 12", callSign: "N12HD", want: []string{"News"}},
		{name: "bbc world news", callSign: "BBCWDNA", want: []string{"News"}},
		{name: "bbc news hd", callSign: "BBCNAHD", want: []string{"News"}},
		{name: "c span", callSign: "CSPANHD", want: []string{"News"}},
		{name: "i24 news", callSign: "I24NEHD", want: []string{"News"}},
		{name: "ytn", callSign: "YTN", want: []string{"News"}},
		{name: "espn", callSign: "ESPNHD", want: []string{"Sports"}},
		{name: "espnews is sports", callSign: "ESPNEWS", want: []string{"Sports"}},
		{name: "msg", callSign: "MSGHD", want: []string{"Sports"}},
		{name: "yes", callSign: "YESHDNY", want: []string{"Sports"}},
		{name: "cbs sports", callSign: "CBSSNHD", want: []string{"Sports"}},
		{name: "nfl network", callSign: "NFLNET", want: []string{"Sports"}},
		{name: "big ten", callSign: "BIG10VH", want: []string{"Sports"}},
		{name: "bein", callSign: "BEIN1HD", want: []string{"Sports"}},
		{name: "local cbs affiliate", callSign: "WCBSHD", affiliate: "CBS", want: nil},
		{name: "local fox affiliate", callSign: "WNYWDT", affiliate: "FOX ENTERTAINMENT", want: nil},
		{name: "bbc america", callSign: "BBCAHD", affiliate: "BBC AMERICA", want: nil},
		{name: "general entertainment", callSign: "TNT", affiliate: "TNT", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyChannelProgramCategories(tt.callSign, tt.affiliate)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("classifyChannelProgramCategories() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannelProgramCategoryRegistrationReturnsCopy(t *testing.T) {
	resetChannelProgramCategoryRegistryForTest()
	defer resetChannelProgramCategoryRegistryForTest()

	RegisterChannelProgramCategories("espn", "ESPNHD", "", "570")
	got := ChannelProgramCategories("espn")
	if !reflect.DeepEqual(got, []string{"Sports"}) {
		t.Fatalf("ChannelProgramCategories() = %v", got)
	}

	got[0] = "Changed"
	again := ChannelProgramCategories("espn")
	if !reflect.DeepEqual(again, []string{"Sports"}) {
		t.Fatalf("registry was mutated through returned slice: %v", again)
	}
}
