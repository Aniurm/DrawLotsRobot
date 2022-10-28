package controller

import (
	"encoding/json"
	"errors"
	"github.com/YasyaKarasu/feishuapi"
	"github.com/sirupsen/logrus"
	"strings"
	"xlab-feishu-robot/app/chat"
	global "xlab-feishu-robot/global"

	"github.com/gin-gonic/gin"
)

var (
	P                    FeishuProjectFormPath
	T                    TemplateDocs
	Url                  UrlStrings
	MyProject            NewProject
	eventForProjectCreat chat.MessageEvent
)

// 向用户发送的链接, 从config读取
type UrlStrings struct {
	UrlHead                  string
	UrlForProjectCreate      string
	UrlForGetUserAccessToken string
	UrlForMeeting            string
}

// 问卷信息，从config读取
type FeishuProjectFormPath struct {
	//立项问卷
	AppTokenForProjectCreat string
	TableIdForProjectCreat  string
	//会议问卷
	AppTokenForMeeting string
	TableIdForMeeting  string
}

// 知识空间模板文件路径
type TemplateDocs struct {
	SpaceId         string
	ParentNodeToken string
}
type Project struct {
	ProjectId int

	// project info
	ProjectName      string
	ProjectType      string // internal | external
	ProjectLeaderIds string // JSON: Array<int> (array of employeeIds)
	GroupId          string //不储存 groupId，因为一个 project 可能对应多个 group

	// doc related info:
	GanttDocUrl   string // 甘特图，其中的排期自动映射到飞书任务
	PrdDocUrl     string // PRD
	TechDocUrl    string // 技术文档
	FeishuRepoUrl string // 飞书知识空间首页

	// status
	ProjectStatus string // beforeStart, pending, revising, hang, finished, aborted
}

type NewProject struct {
	Code int64 `json:"code"`
	Data struct {
		Record struct {
			Fields struct {
				ProjectName          string                `json:"项目名称"`
				ProjectProperties    string                `json:"项目属性"`
				ProjectSource        string                `json:"项目来源"`
				ProjectProfile       string                `json:"项目简介"`
				ParticipatingMembers []ParticipatingMember `json:"主要参与人员"`
				ProjectManager       []ParticipatingMember `json:"产品经理"`
				//CreatTime            string                `json:"创建时间"`
			} `json:"fields"`
			ID       string `json:"id"`
			RecordID string `json:"record_id"`
		} `json:"record"`
	} `json:"data"`
	Msg string `json:"msg"`
}

type ParticipatingMember struct {
	Email  string `json:"email"`
	EnName string `json:"en_name"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

func UnmarshalNewProject(data []byte) (NewProject, error) {
	var r NewProject
	err := json.Unmarshal(data, &r)
	return r, err
}

func (r *NewProject) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

//先向用户发送鉴权链接，等待获取到UserAccessToken，然后再推送立项问卷链接。

func ProjectCreat(event *chat.MessageEvent) {
	msg := "请先查看并点击【机器人私聊会话】中的链接进行用户鉴权，然后填写下方的立项问卷进行立项：\n " + Url.UrlForProjectCreate
	global.Cli.Send("chat_id", event.Message.Chat_id, "text", msg)
	msg = "请点击下面的链接进行鉴权: " + Url.UrlForGetUserAccessToken
	global.Cli.Send("open_id", event.Sender.Sender_id.Open_id, "text", msg)
	eventForProjectCreat = *event
}

func InitProject(c *gin.Context) {
	resp, _ := c.GetRawData()
	temp := make(map[string]string)
	json.Unmarshal(resp, &temp)
	recordId := temp["record_id"]
	data := global.Cli.GetRecordInByte(P.AppTokenForProjectCreat, P.TableIdForProjectCreat, recordId)
	err := json.Unmarshal(data, &MyProject)
	if err != nil {
		panic(err)
	}
	logrus.Info(recordId)
	logrus.Info(MyProject)
	CreateProject()
}

func setProjectType(projectProperties string) string {
	if strings.Contains(projectProperties, "内部") {
		return "internal"
	} else {
		return "external"
	}
}

func getProjectLeaderIds(p ParticipatingMember) string {
	var ProjectLeaderId interface{}
	//TODO:
	//在数据库中查找LeaderId

	//ProjectLeaderId=QueryEmployeeByFullname(p.Name) (*[]Employee, error)
	return "[" + "\"" + ProjectLeaderId.(string) + "\"" + "]"

}

func CreateProject() bool {
	var result bool = false
	if UserAccessToken == "" {
		err := errors.New("UserAccessToken为空，请再次鉴权！")
		panic(err)
		return result
	}
	pjt := MyProject.Data.Record.Fields
	var members []string
	for _, value := range pjt.ParticipatingMembers {
		members = append(members, value.ID)
	}
	manager := pjt.ProjectManager[0].ID

	v := global.Cli.CreateGroup("【"+pjt.ProjectProperties+"】"+pjt.ProjectName, "open_id", manager)
	if v.ChatId != "" {
		logrus.Info("已成功建群：" + v.ChatId)
	}

	//将新建的群插入数据库，
	//InsertGroupRecords(v []Group, "open_id")
	if global.Cli.AddMembers(v.ChatId, "open_id", "1", members) {
		logrus.Info("已成功拉人")
	}

	//创建知识空间
	s := global.Cli.CreateKnowledgeSpace("【"+pjt.ProjectSource+"】"+pjt.ProjectName, pjt.ProjectProfile, "Bearer "+UserAccessToken)
	if v.ChatId != "" {
		logrus.Info("已成功建立知识空间：" + s.SpaceId)
	}
	//将机器人设为管理员
	var botIds []string
	robotId := global.Cli.GetRobotInfo().OpenId
	botIds = append(botIds, robotId)
	global.Cli.AddBotsToKnowledgeSpaceAsAdmin(s.SpaceId, botIds, "Bearer "+UserAccessToken)

	//设置群成员可见
	var chats []string
	chats = append(chats, v.ChatId)
	global.Cli.AddMembersToKnowledgeSpace(s.SpaceId, chats, "openchat")

	//复制节点（生成原始文档）
	//需配置模板文档所在路径
	nodes := global.Cli.GetAllNodes(T.SpaceId, T.ParentNodeToken)
	for _, value := range nodes {
		subNodeParent := global.Cli.CopyNode(T.SpaceId, value.NodeToken, s.SpaceId, "", value.Title)
		if value.HasChild {
			n := global.Cli.GetAllNodes(T.SpaceId, value.NodeToken)
			for _, v := range n {
				global.Cli.CopyNode(T.SpaceId, v.NodeToken, s.SpaceId, subNodeParent.NodeToken, v.Title)
			}
		}

	}
	logrus.Info("已成功在知识空间建立初始文档")

	//插入项目信息至数据库
	/*
		projectInfo := Project{
			ProjectId:        0,
			ProjectName:      pjt.ProjectName,
			ProjectType:      setProjectType(pjt.ProjectProperties),
			ProjectLeaderIds: getProjectLeaderIds(pjt.ProjectManager[0]),
			//GroupId:          GetNewGroupId(),
			GanttDocUrl:   "",
			PrdDocUrl:     "",
			TechDocUrl:    "",
			FeishuRepoUrl: "",
			ProjectStatus: "",
		}
		var projectInfoList []Project
		projectInfoList = append(projectInfoList, projectInfo)
		InsertProjectRecords(projectInfoList)
	*/
	//添加映射
	global.Rob.GroupSpace[v.ChatId] = s.SpaceId
	global.Rob.GroupOwner[v.ChatId] = manager

	//启动Timer
	StartGroupTimer(v.ChatId)

	//提醒项目经理填表
	SendProjectManageUrl(s)

	result = true
	//清除变量，为下一次立项准备
	UserAccessToken = ""
	//p := reflect.ValueOf(MyProject).Elem()
	//p.Set(reflect.Zero(p.Type()))

	return result
}

func in(target string, str_array []string) bool {
	for _, element := range str_array {
		if target == element {
			return true
		}
	}
	return false
}

func SendProjectManageUrl(s *feishuapi.SpaceInfo) {
	msg := "请项目经理填写甘特图、排期表、任务进度管理：\n"
	var titles []string
	titles = append(titles, "排期甘特图", "项目会议", "任务进度管理")

	nodes := global.Cli.GetAllNodes(s.SpaceId)
	for _, value := range nodes {
		if in(value.Title, titles) {
			msg = msg + Url.UrlHead + value.NodeToken + " \n"
		}
		if value.HasChild {
			n := global.Cli.GetAllNodes(T.SpaceId, value.NodeToken)
			for _, v := range n {
				if in(v.Title, titles) {
					msg = msg + Url.UrlHead + v.NodeToken + "\n"
				}
			}
		}

	}

	global.Cli.Send("chat_id", eventForProjectCreat.Message.Chat_id, "text", msg)

}