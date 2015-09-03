package archiver

import (
	"gopkg.in/mgo.v2/bson"
	"sort"
)

type smapProperties struct {
	unitOfTime    UnitOfTime
	unitOfMeasure string
	streamType    StreamType
}

func (sp smapProperties) IsEmpty() bool {
	return sp.unitOfTime == 0 &&
		sp.unitOfMeasure == "" &&
		sp.streamType == 0
}

type SmapMessage struct {
	Path       string
	UUID       UUID `json:"uuid"`
	Properties smapProperties
	Actuator   Dict
	Metadata   Dict
	Readings   []Reading
}

// returns this struct as BSON for storing the metadata. We ignore Readings
// because they are not part of the metadata store
//TODO: explore putting this in the mongo-specific file? This isn't general purpose
func (msg *SmapMessage) ToBson() (ret bson.M) {
	ret = bson.M{
		"uuid": msg.UUID,
		"Path": msg.Path,
	}
	if msg.Metadata != nil && len(msg.Metadata) > 0 {
		for k, v := range msg.Metadata {
			ret["Metadata."+fixKey(k)] = v
		}
	}
	if msg.Actuator != nil && len(msg.Actuator) > 0 {
		for k, v := range msg.Actuator {
			ret["Actuator."+fixKey(k)] = v
		}
	}
	if !msg.Properties.IsEmpty() {
		ret["Properties.UnitofTime"] = msg.Properties.unitOfTime
		ret["Properties.UnitofMeasure"] = msg.Properties.unitOfMeasure
		ret["Properties.StreamType"] = msg.Properties.streamType
	}
	return ret
}

func SmapMessageFromBson(m bson.M) *SmapMessage {
	ret := &SmapMessage{}
	if uuid, found := m["uuid"]; found {
		ret.UUID = UUID(uuid.(string))
	}

	if path, found := m["Path"]; found {
		ret.Path = path.(string)
	}

	if md, found := m["Metadata"]; found {
		ret.Metadata = *DictFromBson(md.(bson.M))
	}

	if md, found := m["Actuator"]; found {
		ret.Actuator = *DictFromBson(md.(bson.M))
	}

	if md, found := m["Properties"]; found {
		if props, ok := md.(bson.M); ok {
			ret.Properties = smapProperties{}
			if uot, fnd := props["UnitofTime"]; fnd {
				ret.Properties.unitOfTime = uot.(UnitOfTime)
			}
			if uom, fnd := props["UnitofMeasure"]; fnd {
				ret.Properties.unitOfMeasure = uom.(string)
			}
			if uot, fnd := props["StreamType"]; fnd {
				ret.Properties.streamType = uot.(StreamType)
			}
		}
	}

	return ret
}

// returns True if the message contains anything beyond Path, UUID, Readings
func (msg *SmapMessage) HasMetadata() bool {
	return (msg.Actuator != nil && len(msg.Actuator) > 0) ||
		(msg.Metadata != nil && len(msg.Metadata) > 0) ||
		(!msg.Properties.IsEmpty())
}

func (msg *SmapMessage) IsTimeseries() bool {
	return msg.UUID != ""
}

type SmapMessageList []*SmapMessage

func (sml *SmapMessageList) ToBson() []bson.M {
	ret := make([]bson.M, len(*sml))
	for idx, msg := range *sml {
		ret[idx] = msg.ToBson()
	}
	return ret
}

func SmapMessageListFromBson(m []bson.M) *SmapMessageList {
	ret := make(SmapMessageList, len(m))
	for idx, doc := range m {
		ret[idx] = SmapMessageFromBson(doc)
	}
	return &ret
}

type TieredSmapMessage map[string]*SmapMessage

// This performs the metadata inheritance for the paths and messages inside
// this collection of SmapMessages. Inheritance starts from the root path "/"
// can progresses towards the leaves.
// First, get a list of all of the potential timeseries (any path that contains a UUID)
// Then, for each of the prefixes for the path of that timeserie (util.getPrefixes), grab
// the paths from the TieredSmapMessage that match the prefixes. Sort these in "decreasing" order
// and apply to the metadata.
// Finally, delete all non-timeseries paths
func (tsm *TieredSmapMessage) CollapseToTimeseries() {
	var (
		prefixMsg *SmapMessage
		found     bool
	)
	for path, msg := range *tsm {
		if !msg.IsTimeseries() {
			continue
		}
		prefixes := getPrefixes(path)
		sort.Sort(sort.Reverse(sort.StringSlice(prefixes)))
		for _, prefix := range prefixes {
			// if we don't find the prefix OR it exists but doesn't have metadata, we skip
			prefixMsg, found = (*tsm)[prefix]
			if !found || prefixMsg == nil || (prefixMsg != nil && !prefixMsg.HasMetadata()) {
				continue
			}
			// otherwise, we apply keys from paths higher up if our timeseries doesn't already have the key
			// (this is reverse inheritance)
			if prefixMsg.Metadata != nil && len(prefixMsg.Metadata) > 0 {
				for k, v := range prefixMsg.Metadata {
					if _, hasKey := msg.Metadata[k]; !hasKey {
						if msg.Metadata == nil {
							msg.Metadata = make(Dict)
						}
						msg.Metadata[k] = v
					}
				}
			}
			if !prefixMsg.Properties.IsEmpty() {
				if msg.Properties.unitOfTime != 0 {
					msg.Properties.unitOfTime = prefixMsg.Properties.unitOfTime
				}
				if msg.Properties.unitOfMeasure != "" {
					msg.Properties.unitOfMeasure = prefixMsg.Properties.unitOfMeasure
				}
				if msg.Properties.streamType != 0 {
					msg.Properties.streamType = prefixMsg.Properties.streamType
				}
			}

			if prefixMsg.Actuator != nil && len(prefixMsg.Actuator) > 0 {
				for k, v := range prefixMsg.Actuator {
					if _, hasKey := msg.Actuator[k]; !hasKey {
						if msg.Actuator == nil {
							msg.Actuator = make(Dict)
						}
						msg.Actuator[k] = v
					}
				}
			}
			(*tsm)[path] = msg
		}
	}
	// when done, delete all non timeseries paths
	for path, msg := range *tsm {
		if !msg.IsTimeseries() {
			delete(*tsm, path)
		}
	}
}
