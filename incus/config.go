package incus

import (
	"log"
	"strconv"

	//"github.com/briankassouf/cfg"
)

type Configuration struct {
	vars map[string]string
}

func InitConfig(mymap map[string]string) Configuration {
	//mymap := make(map[string]string)
	/*	err := cfg.Load("/etc/incus/incus.conf", mymap)
		if err != nil {
			log.Panic(err)
		}*/

	return Configuration{mymap}
}

func (this *Configuration) Get(name string) string {
	val, ok := this.vars[name]
	if !ok {
		log.Panicf("Config Error: variable '%s' not found", name)
	}

	return val
}

func (this *Configuration) GetInt(name string) int {
	val, ok := this.vars[name]
	if !ok {
		log.Panicf("Config Error: variable '%s' not found", name)
	}

	i, err := strconv.Atoi(val)
	if err != nil {
		log.Panicf("Config Error: '%s' could not be cast as an int", name)
	}

	return i
}

func (this *Configuration) GetBool(name string) bool {
	val, ok := this.vars[name]
	if !ok {
		return false
	}

	return val == "true"
}
