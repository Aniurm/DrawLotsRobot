package chat

import (
	"xlab-feishu-robot/global"

	_ "github.com/sirupsen/logrus"
)

func init() {
	GroupMessageRegister(groupHelpMenu, "help")
}

func groupHelpMenu(messageevent *MessageEvent) {
	global.Cli.Send("chat_id", messageevent.Message.Chat_id, "text", "this is a group test string")
}
