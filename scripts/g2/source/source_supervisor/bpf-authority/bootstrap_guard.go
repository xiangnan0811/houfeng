//go:build linux && amd64 && !cgo

package main

func bootstrapDrainRecords(g guardDevice, recorder *bootstrapRecorder, cursor *uint64) ([]guardRecord, error) {
	return bootstrapDrainObserved(g, recorder, cursor, nil)
}

func bootstrapDrainObserved(g guardDevice, recorder *bootstrapRecorder, cursor *uint64, observe func(guardRecord) error) ([]guardRecord, error) {
	if recorder == nil || cursor == nil {
		return nil, errGuardProtocol
	}
	var result []guardRecord
	for {
		batch, err := g.readEvents(64, *cursor, 0)
		if err != nil {
			return nil, err
		}
		if batch.Sticky != 0 {
			return nil, errGuardProtocol
		}
		if len(batch.Records) == 0 {
			return result, nil
		}
		for _, record := range batch.Records {
			value, err := record.projection()
			if err != nil {
				return nil, err
			}
			if err = recorder.add("guard", map[string]any{"record": value}); err != nil {
				return nil, err
			}
			recorder.guardRecords = append(recorder.guardRecords, record)
			*cursor = record.Sequence
			if observe != nil {
				if err := observe(record); err != nil {
					return nil, err
				}
			}
			result = append(result, record)
		}
	}
}
