/*
 * Project: raft-lite
 * ---------------------
 * Authors:
 *   Minjian Chen 813534
 *   Shijie Liu   813277
 *   Weizhi Xu    752454
 *   Wenqing Xue  813044
 *   Zijun Chen   813190
 */

package client

import (
	"github.com/PwzXxm/raft-lite/rpccore"
	"github.com/PwzXxm/raft-lite/utils"
	"github.com/gofrs/flock"
	"github.com/pkg/errors"
)

type clientConfig struct {
	NodeAddrMap map[rpccore.NodeID]string
	ClientID    string
}

// StartClientFromFile starts Client from given file
func StartClientFromFile(filepath string) error {
	var config clientConfig
	err := utils.ReadClientFromJSON(&config, filepath)
	if err != nil {
		return err
	}

	fl := flock.New(filepath)
	if locked, _ := fl.TryLock(); !locked {
		return errors.New("Unable to lock the config file," +
			" make sure there isn't another instance running.")
	}
	defer func() {
		_ = fl.Unlock()
	}()

	c, err := NewClientFromConfig(config)
	if err != nil {
		return err
	}

	c.startReadingCmd()
	c.net.Shutdown()
	return nil
}
