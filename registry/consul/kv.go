package consul

import (
	"strings"

	"github.com/hashicorp/consul/api"
)

func getKV(client *api.Client, key string, waitIndex uint64) (string, uint64, error) {
	q := &api.QueryOptions{RequireConsistent: true, WaitIndex: waitIndex}
	kvpair, meta, err := client.KV().Get(key, q)
	if err != nil {
		return "", 0, err
	}
	if kvpair == nil {
		return "", meta.LastIndex, nil
	}
	return strings.TrimSpace(string(kvpair.Value)), meta.LastIndex, nil
}

func putKV(client *api.Client, key, value string, index uint64) (bool, error) {
	p := &api.KVPair{Key: strings.TrimPrefix(key, "/"), Value: []byte(value), ModifyIndex: index}
	ok, _, err := client.KV().CAS(p, nil)
	if err != nil {
		return false, err
	}
	return ok, nil
}
