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
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PwzXxm/raft-lite/rpccore"
	"github.com/PwzXxm/raft-lite/sm"
	"github.com/PwzXxm/raft-lite/utils"
	"github.com/common-nighthawk/go-figure"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Client representing the client
type Client struct {
	net  *rpccore.TCPNetwork
	core ClientCore
}

// ClientCore representing the detailed info of clietn
type ClientCore struct {
	ActBuilder *sm.TSMActionBuilder

	clientID        string
	leaderID        *rpccore.NodeID
	nl              []rpccore.NodeID
	node            rpccore.Node
	logger          *logrus.Logger
	backOffDuration int
}

const (
	maxBackOffDuration       = 1600 // ms
	initBackOffDuration      = 20   // ms
	maxCheckCountBeforeRetry = 6
)

const (
	tcpTimeout        = time.Second
	cmdQuery          = "query"
	cmdSet            = "set"
	cmdIncre          = "increment"
	cmdMove           = "move"
	cmdSetLoggerLevel = "loggerLevel"
	loggerLevelDebug  = "debug"
	loggerLevelInfo   = "info"
	loggerLevelWarn   = "warn"
	loggerLevelError  = "error"
)

// command usage maps
var usageMp = map[string]string{
	cmdQuery:          "<key>",
	cmdSet:            "<key> <value>",
	cmdIncre:          "<key> <value>",
	cmdMove:           "<source> <target> <value>",
	cmdSetLoggerLevel: "<level> (warn, info, debug, error)",
}

// NewClientFromConfig returns a new Client from a given configuration
func NewClientFromConfig(config clientConfig) (*Client, error) {
	c := new(Client)

	// using TCP network with duration of tcpTimeout (1 second)
	c.net = rpccore.NewTCPNetwork(tcpTimeout)

	// create Client's TCPNode in local using Client ID
	cnode, err := c.net.NewLocalClientOnlyNode(rpccore.NodeID(config.ClientID))
	if err != nil {
		return nil, err
	}

	// create a list of nodes with given node addresses
	nl := make([]rpccore.NodeID, len(config.NodeAddrMap))
	i := 0
	for nodeID, addr := range config.NodeAddrMap {
		nl[i] = nodeID
		i++
		// add all node addresses as remote ndoes
		err := c.net.NewRemoteNode(nodeID, addr)
		if err != nil {
			return nil, err
		}
	}

	// set up logger
	logger := logrus.New()
	logger.Out = os.Stdout
	logger.SetLevel(logrus.InfoLevel)

	// initialize the client core
	c.core = NewClientCore(config.ClientID, nl, cnode, logger)

	return c, nil
}

// NewClientCore takes arguments and returns a ClientCore object
func NewClientCore(clientID string, nodeIDs []rpccore.NodeID, cnode rpccore.Node, logger *logrus.Logger) ClientCore {
	return ClientCore{
		clientID:        clientID,
		leaderID:        nil,
		nl:              nodeIDs,
		ActBuilder:      sm.NewTSMActionBuilder(clientID),
		node:            cnode,
		logger:          logger,
		backOffDuration: initBackOffDuration,
	}
}

// client starts reading the commands
func (c *Client) startReadingCmd() {
	printWelcomeMsg()

	invalidCommandError := errors.New("Invalid command")
	var err error

	// set colours
	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)
	green.Print("> ")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		cmd := strings.Fields(scanner.Text())

		err = nil
		l := len(cmd)

		if l == 0 {
			err = errors.New("Command cannot be empty")
		}

		if err == nil {
			switch cmd[0] {
			// query command
			case cmdQuery:
				if l != 2 {
					err = combineErrorUsage(invalidCommandError, cmd[0])
					break
				}
				res, err := c.core.ExecuteQueryRequest(sm.NewTSMDataQuery(cmd[1]))
				if err != nil {
					_, _ = red.Println(err)
				} else {
					green.Printf("The query result for key %v: %v\n", cmd[1], res)
				}
			// logger command: debug, info, warn, error
			case cmdSetLoggerLevel:
				if l != 2 {
					err = combineErrorUsage(invalidCommandError, cmd[0])
					break
				}
				switch cmd[1] {
				case loggerLevelDebug:
					c.core.logger.SetLevel(logrus.DebugLevel)
					_, _ = green.Println("Logger level set to debug")
				case loggerLevelInfo:
					c.core.logger.SetLevel(logrus.InfoLevel)
					_, _ = green.Println("Logger level set to info")
				case loggerLevelWarn:
					c.core.logger.SetLevel(logrus.WarnLevel)
					_, _ = green.Println("Logger level set to warn")
				case loggerLevelError:
					c.core.logger.SetLevel(logrus.ErrorLevel)
					_, _ = green.Println("Logger level set to error")
				default:
					err = combineErrorUsage(invalidCommandError, cmd[0])
				}
			// set and increment command
			case cmdSet, cmdIncre:
				if l != 3 {
					err = combineErrorUsage(invalidCommandError, cmd[0])
					break
				}
				value, e := strconv.Atoi(cmd[2])
				if e != nil {
					err = errors.New("value should be an integer")
					break
				}
				switch cmd[0] {
				case cmdSet:
					c.executeActionRequestAndPrint(c.core.ActBuilder.TSMActionSetValue(cmd[1], value))
				case cmdIncre:
					c.executeActionRequestAndPrint(c.core.ActBuilder.TSMActionIncrValue(cmd[1], value))
				}
			// move command
			case cmdMove:
				if l != 4 {
					err = combineErrorUsage(invalidCommandError, cmd[0])
					break
				}
				value, e := strconv.Atoi(cmd[3])
				if e != nil {
					err = errors.New("value should be an integer")
					break
				}
				c.executeActionRequestAndPrint(c.core.ActBuilder.TSMActionMoveValue(cmd[1], cmd[2], value))
			default:
				_, _ = red.Fprintln(os.Stderr, invalidCommandError)
				utils.PrintUsage(usageMp)
			}
		}
		if err != nil {
			_, _ = red.Fprintln(os.Stderr, err)
		}
		green.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Failed reading stdout: ", err)
	}
}

// client executes the action request and prints result messages
func (c *Client) executeActionRequestAndPrint(act sm.TSMAction) {
	success, msg := c.core.ExecuteActionRequest(act)
	var ca color.Attribute
	if success {
		ca = color.FgGreen
	} else {
		ca = color.FgHiRed
	}
	_, _ = color.New(ca).Println(msg)
}

// combineErrorUsage returns error usage message
func combineErrorUsage(e error, cmd string) error {
	return errors.New(e.Error() + "\nUsage: " + cmd + " " + usageMp[cmd])
}

func (core *ClientCore) lookForLeader() rpccore.NodeID {
	// cached, the cache will be cleaned if there is any issue
	// blocking, keep trying until find a leader
	for core.leaderID == nil {
		// select a client by random
		pl := core.nl[utils.Random(0, len(core.nl)-1)]
		var leaderRes LeaderRes
		err := callRPC(core, pl, RPCMethodLeaderRequest, "", &leaderRes)
		if err == nil {
			if leaderRes.HasLeader {
				core.logger.Infof("Node %v answered with leader = %v", pl,
					leaderRes.LeaderID)
				core.leaderID = &leaderRes.LeaderID
				resetBackOffDuration(core)
				return *core.leaderID
			}
			err = errors.Errorf("Node %v doesn't know the leader.", pl)
		}
		core.logErrAndBackoff("Unable to find leader. ", err)
	}
	return *core.leaderID
}

// resetBackOffDuration resets the backOffDuration
func resetBackOffDuration(core *ClientCore) {
	core.backOffDuration = initBackOffDuration
}

// logErrAndBackoff takes ClientCore pointer, message string and error value
func (core *ClientCore) logErrAndBackoff(msg string, err error) {
	core.leaderID = nil
	core.logger.Debug(msg, err)

	// this function can only be called when one action failed
	// thus, only one counter is necessary
	time.Sleep(time.Duration(core.backOffDuration) * time.Millisecond)

	core.backOffDuration = utils.Min(maxBackOffDuration, core.backOffDuration*2)
}

// sendActionRequest takes ClientCore and ActionReq structs as arguments,
// calls action request RPC, and returns error value if occurs
func (core *ClientCore) sendActionRequest(actReq ActionReq) error {
	leader := core.lookForLeader()
	var actionRes ActionRes
	err := callRPC(core, leader, RPCMethodActionRequest, actReq, &actionRes)
	if err == nil && !actionRes.Started {
		err = errors.Errorf("Node %v declined the request.", leader)
	}
	return err
}

// checkActionRequest takes ClientCore and QueryReq structs as arguments,
// calls query request RPC and returns a TSMRequestInfo pointer if success
func (core *ClientCore) checkActionRequest(queryReq QueryReq) (*sm.TSMRequestInfo, error) {
	leader := core.lookForLeader()
	var queryRes QueryRes
	err := callRPC(core, leader, RPCMethodQueryRequest, queryReq, &queryRes)
	if err == nil {
		if queryRes.Success {
			if queryRes.QueryErr == nil {
				info := queryRes.Data.(sm.TSMRequestInfo)
				return &info, nil
			}
			// query success, but there is no related request info
			return nil, nil
		}
		err = errors.Errorf("Node %v decliend the query request.", leader)
	}
	return nil, err
}

// ExecuteActionRequest takes ClientCore and TSMAction structs as arguments,
// and returns whether the action is succeed and error value if occurs
func (core *ClientCore) ExecuteActionRequest(act sm.TSMAction) (bool, string) {
	actReq := ActionReq{Cmd: act}
	queryReq := QueryReq{Cmd: sm.NewTSMLatestRequestQuery(core.clientID)}
	reqID := act.GetRequestID()
	for {
		err := core.sendActionRequest(actReq)
		if err != nil {
			core.logErrAndBackoff("send action request failed. ", err)
			continue
		}
		resetBackOffDuration(core)

		for i := 0; i < maxCheckCountBeforeRetry; i++ {
			info, err := core.checkActionRequest(queryReq)
			if err != nil {
				core.logErrAndBackoff("check action request failed. ", err)
			}
			// RequestInfo exists and RequestID matches
			if info != nil && info.RequestID == reqID {
				resetBackOffDuration(core)
				if info.Err != nil {
					return false, *info.Err
				}
				return true, "action success"
			}
			if err == nil {
				core.logErrAndBackoff("info is nil or wrong request ID", err)
			}
		}
	}
}

// ExecuteQueryRequest takes ClientCore and TSMQuery structs as arguments,
// and returns data from the query response and error value if occurs
func (core *ClientCore) ExecuteQueryRequest(query sm.TSMQuery) (interface{}, error) {
	queryReq := QueryReq{Cmd: query}
	for {
		leader := core.lookForLeader()
		var queryRes QueryRes
		// call query request RPC
		err := callRPC(core, leader, RPCMethodQueryRequest, queryReq, &queryRes)
		if err == nil {
			if queryRes.Success {
				resetBackOffDuration(core)
				if queryRes.QueryErr == nil {
					return queryRes.Data, nil
				}
				// query success, but query error exists
				return nil, errors.New(*queryRes.QueryErr)
			}
			err = errors.Errorf("Node %v decliend the query request.", leader)
		}
		if err != nil {
			core.logErrAndBackoff("Request query failed. ", err)
			continue
		}
	}
}

// printWelcomeMsg prints welcome message
func printWelcomeMsg() {
	fmt.Printf("\n=============================================\n")
	figure.NewFigure("Raft lite", "doom", true).Print()
	fmt.Printf("\n\n Welcome to Raft Lite Transaction System\n")
	fmt.Printf("\n=============================================\n")
}
