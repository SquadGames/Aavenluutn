package publisher

import (
	"math/big"
	"time"
	"fmt"
	"net/http"
	"net/url"
	"io"
	"strings"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	v8 "rogchap.com/v8go"
)

type PublisherStore interface {
	Now() time.Time

	Set(key string, value ScriptResult) error
	Get(key string) (ScriptResult, error)

	GetFullStateForTest() map[string]ScriptResult

	LatestBlockTimeAsOf(t time.Time) (time.Time, error)
	NextBlockTimeAsOf(t time.Time) (time.Time, error)
}

func (p *Publisher) GetNarrator(i int64) (PublisherNarrator, error) {
	return p.Narrators(nil, big.NewInt(i))
}

func (p *Publisher) GetScriptURI(i int64, backend bind.ContractBackend) (*url.URL, error) {
	narrator, err := p.GetNarrator(i)
	if err != nil {
		return &url.URL{}, err
	}
	nft, err := NewNarratorNFTs(narrator.NFTAddress, backend)
	if err != nil {
		return &url.URL{}, err
	}
	uriString, err := nft.TokenURI(nil, narrator.NFTId); if err != nil {
		return &url.URL{}, err
	}
	return url.Parse(uriString)
}

var scriptCache string
func (p *Publisher) GetScript(i int64, backend bind.ContractBackend) (string, error) {
	if scriptCache != "" {
		return scriptCache, nil
	}
	scriptURI, err := p.GetScriptURI(i, backend); if err != nil {
		return "", err
	}

	switch scriptURI.Scheme {
	case "data":
		_, script, err := parseOpaqueData(scriptURI.Opaque); if err != nil {
			return "", err
		}
		return script, nil
	default:
		resp, err := http.Get(scriptURI.String()); if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return string(body), nil
	}
}

func (pub *Publisher) GetStory(
	backend bind.ContractBackend,
	ps PublisherStore,
	narratorIndex int64,
	collectionIndex int64,
	storyIndex int64,
) (Story, error) {
	return pub.GetStoryAsOf(
		backend,
		ps,
		narratorIndex,
		collectionIndex,
		storyIndex,
		ps.Now(),
	)
}

func LatestBlockTime(ps PublisherStore) (time.Time, error) {
	return ps.LatestBlockTimeAsOf(ps.Now())
}

func GetStateKey(
	ps PublisherStore,
	narrator int64,
	collection int64,
	story int64,
	t time.Time,
) string {
	return fmt.Sprintf("%v.%v.%v.%v", narrator, collection,	story, t.Unix())
}

func GetCachedResult(
	ps PublisherStore,
	narratorIndex int64,
	collectionIndex int64,
	storyIndex int64,
	t time.Time,
) (ScriptResult, error) {
	stateKey := GetStateKey(ps, narratorIndex, collectionIndex, storyIndex, t)
	result, err := ps.Get(stateKey)
	return result, err
}

func parseOpaqueData(opaque string) (string, string, error) {
	mediaTypeSplit := strings.Split(opaque, ",")
	if len(mediaTypeSplit) < 2 {
		return "", "", errors.New("Missing data URI media type")
	}
	mediaType := mediaTypeSplit[0]
	data := strings.Join(mediaTypeSplit[1:], ",")
	if strings.Index(mediaType, ";base64") != -1 {
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return "", "", err
		}
		data = string(decoded)
	}
	return strings.Split(mediaType, ";")[0], data, nil
}

type Story = interface{}

type ScriptResult struct {
	Stories []Story `json:"stories"`
	NextState map[string]interface{} `json:"nextState"`
}

// RunNarratorScript run a narrator script in V8, return next state and some stories
func (pub *Publisher) RunNarratorScript(
	script string,
	previousResult string,
	collectionStart int64,
	collectionLength int64,
	collectionSize int64,
) (ScriptResult, error) {
	jsVM, _ := v8.NewContext()

	// script must define a tellStory function that takes an initial state
	// and returns { state: mapping, stories: string[] }
	if _, err := jsVM.RunScript(script, "index.js"); err != nil {
		return ScriptResult{}, err
	}

	functionCall := fmt.Sprintf(
		"tellStory(%v, %v, %v, %v, %v)",
		previousResult,
		collectionStart,
		collectionLength,
		collectionSize,
		"'localhost:8545'", // todo make providerURL configurable?
	)

	resultJSValue, err := jsVM.RunScript(functionCall, "index.js")
	if err != nil {
		return ScriptResult{}, err
	}

	resultJSON, err := json.Marshal(resultJSValue); if err != nil {
		return ScriptResult{}, err
	}

	var result ScriptResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		return ScriptResult{}, err
	}
	return result, nil
}

// before compares a go time.Time and a big.Int from the contract
// intrepreted as a unix timestamp
func before(a time.Time, b *big.Int) bool {
	bTime := time.Unix(b.Int64(), 0)
	return a.Before(bTime)
}

var depth = 0
func (pub *Publisher) GetStoryAsOf(
	backend bind.ContractBackend,
	ps PublisherStore,
	narratorIndex int64,
	collectionIndex int64,
	storyIndex int64,
	t time.Time,
) (Story, error) {
	depth += 1
	latestBlockTime, err := ps.LatestBlockTimeAsOf(t);
	if t.Before(latestBlockTime) {
		panic(errors.New("why tho"))
	}

	if err != nil {
		depth -= 1
		return "", err
	}

	narrator, err := pub.GetNarrator(narratorIndex); if err != nil {
		depth -= 1
		return "", err
	}

	// if latestBlockTime is before the narrator start time
	// use `{}` as the state
	var previousResult ScriptResult
	previousResult, err = GetCachedResult(
		ps,
		narratorIndex,
		collectionIndex,
		storyIndex,
		latestBlockTime,
	); if err != nil && !before(latestBlockTime, narrator.Start) {
		// We don't have the result in the cache...
		// and it's after the start time...
		// so we need to get it by recursing and getting the state
		// as of a time just before the most recent block time
		pub.GetStoryAsOf(
			backend,
			ps,
			narratorIndex,
			collectionIndex,
			storyIndex,
			latestBlockTime.Add(time.Second * -1), // a second before
		)
	}
	var state string
	if before(latestBlockTime, narrator.Start) {
		state = "{}"
	} else {
		marshaled, err := json.Marshal(previousResult.NextState); if err != nil {
			depth -= 1
			return "", err
		}
		state = string(marshaled)
	}

	collectionStart := narrator.Start.Int64() +
		narrator.CollectionSpacing.Int64() * collectionIndex

	script, err := pub.GetScript(narratorIndex, backend); if err != nil {
		depth -= 1
		return "", err
	}
	result, err := pub.RunNarratorScript(
		script,
		state,
		collectionStart,
		narrator.CollectionLength.Int64(),
		narrator.CollectionSize.Int64(),
	); if err != nil {
		depth -= 1
		return "", err
	}

	// This has to save for the next future block time
	// not the latest block time
	nextBlockTime, noNextBlockErr := ps.NextBlockTimeAsOf(t)

	if noNextBlockErr == nil {
		// if there is a next block, save this result against it
		stateKey := GetStateKey(
			ps,
			narratorIndex,
			collectionIndex,
			storyIndex,
			nextBlockTime,
		)
		ps.Set(stateKey, result)
	}
	depth -= 1
	return result.Stories[storyIndex], nil
}
