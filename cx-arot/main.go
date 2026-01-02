package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
)

// ChaoxingClient 用于管理超星平台的 HTTP 会话和用户状态。
// 包含已认证的 HTTP 客户端、用户 ID (uid) 和表单 ID (fid)。
type ChaoxingClient struct {
	http *resty.Client
	uid  string
	fid  string
}

// NewClient 初始化一个新的 ChaoxingClient。
// 设置了默认的 User-Agent、超时时间和重试机制。
func NewClient() *ChaoxingClient {
	r := resty.New().
		SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36").
		SetTimeout(10 * time.Second).
		SetRetryCount(3)

	return &ChaoxingClient{http: r}
}

// Login 执行登录流程。
// 成功后会自动解析响应头，提取后续请求必须的 UID 和 fid Cookie。
func (c *ChaoxingClient) Login(username, password string) error {
	// 模拟登录表单提交
	resp, err := c.http.R().
		SetFormData(map[string]string{
			"fid":      "-1",
			"uname":    username,
			"password": password,
			"referer":  "http://i.mooc.chaoxing.com/space/index",
			"t":        "true",
		}).Post("https://passport2.chaoxing.com/fanyalogin")

	if err != nil {
		return fmt.Errorf("网络请求失败: %w", err)
	}

	// 使用 gjson 快速检查登录状态
	if !gjson.GetBytes(resp.Body(), "status").Bool() {
		msg := gjson.GetBytes(resp.Body(), "msg2").String()
		return fmt.Errorf("登录失败: %s", msg)
	}

	// 从 CookieJar 中提取关键的 UID 和 fid
	for _, ck := range c.http.GetClient().Jar.Cookies(resp.Request.RawRequest.URL) {
		switch ck.Name {
		case "UID":
			c.uid = ck.Value
		case "fid":
			c.fid = ck.Value
		}
	}
	return nil
}

// GetSemesterID 获取当前学期的 ID。
func (c *ChaoxingClient) GetSemesterID() (string, error) {
	resp, err := c.http.R().Get("https://newes.chaoxing.com/pj/semesterV2/findSemesterList")
	if err != nil {
		return "", fmt.Errorf("获取学期列表失败: %w", err)
	}

	// 使用 gjson 路径语法查找 iscurrent==1 的元素 ID
	id := gjson.GetBytes(resp.Body(), "data.#[iscurrent==1].id").String()
	if id == "" {
		return "", fmt.Errorf("未找到当前学期")
	}
	return id, nil
}

// ProcessQuestionnaires 获取指定学期的所有问卷，并并发处理。
// 使用 sync.WaitGroup 实现并发控制，提高多门课程评教的速度。
func (c *ChaoxingClient) ProcessQuestionnaires(semesterID string) {
	resp, err := c.http.R().
		SetQueryParams(map[string]string{
			"semesterId": semesterID,
			"pageIndex":  "1",
			"pageSize":   "20",
		}).
		Get("https://newes.chaoxing.com/pj/newesReceptionV2/GetMyEvaluationList")

	if err != nil {
		log.Printf("[错误] 获取问卷列表失败: %v", err)
		return
	}

	list := gjson.GetBytes(resp.Body(), "data.list").Array()
	var wg sync.WaitGroup

	log.Printf("[信息] 发现 %d 份问卷，开始执行任务...", len(list))

	for _, q := range list {
		qID := q.Get("questionnaireId").String()
		qName := q.Get("name").String()

		wg.Add(1)
		// 为每个问卷启动一个 goroutine
		go func(qid, name string) {
			defer wg.Done()
			c.evaluateQuestionnaire(qid, name)
		}(qID, qName)
	}

	wg.Wait()
}

// evaluateQuestionnaire 处理单个问卷的具体逻辑。
// 遍历问卷中的所有课程/教师，跳过已提交的项目，对未提交的进行评教。
func (c *ChaoxingClient) evaluateQuestionnaire(qID, qName string) {
	// API 要求 ID 不带引号，清理一下
	cleanQID := strings.Trim(qID, "\"")

	// 获取该问卷下的所有待评列表 (pageSize 设置大一点以获取全部)
	resp, err := c.http.R().
		SetQueryParams(map[string]string{
			"questionnaireId": cleanQID,
			"pageIndex":       "1",
			"pageSize":        "1500",
		}).
		Get("https://newes.chaoxing.com/pj/newesReceptionV2/GetMyEvaluationQuestionnaireById")

	if err != nil {
		log.Printf("[%s] 获取评价列表失败: %v", qName, err)
		return
	}

	evals := gjson.GetBytes(resp.Body(), "data.list").Array()
	for _, item := range evals {
		// 跳过 submitStatus 为 2 (已提交) 的项目
		if item.Get("submitStatus").Int() == 2 {
			continue
		}

		// === 获取详细信息 ===
		teacher := item.Get("alreadyObject.teacherName").String()        // 教师姓名
		course := item.Get("alreadyObject.courseName").String()          // 课程名称
		dept := item.Get("alreadyObject.teacherDepartmentName").String() // 学院名称

		alreadyID := item.Get("alreadyObjectId").String()
		grantID := item.Get("grantId").String()

		log.Printf("[%s] 正在评教: %s - %s - %s", qName, dept, course, teacher)

		if err := c.submitEvaluation(cleanQID, alreadyID, grantID); err != nil {
			log.Printf("❌ [%s] %s (%s) 评教失败: %v", qName, teacher, course, err)
		} else {
			log.Printf("✅ [%s] %s (%s) 评教成功", qName, teacher, course)
		}

		// 稍微延时，避免请求过快触发风控
		time.Sleep(500 * time.Millisecond)
	}
}

// submitEvaluation 获取评教页面 HTML，解析题目 Token，并构造数据提交。
// 逻辑还原了 Python 版本：区分主观题（留空）和客观题（打满分），并过滤无效 ID。
func (c *ChaoxingClient) submitEvaluation(qID, alreadyID, grantID string) error {
	// 获取 HTML 页面
	htmlResp, err := c.http.R().
		SetQueryParams(map[string]string{
			"fid":             c.fid,
			"uId":             c.uid,
			"questionnaireId": qID,
			"alreadyId":       alreadyID,
			"grantId":         grantID,
			"type":            "1",
			"isNewPage":       "true",
			"source":          "14",
		}).Get("https://newes.chaoxing.com/pj/newesReception/questionnaireInfo")

	if err != nil {
		return fmt.Errorf("加载表单 HTML 失败: %w", err)
	}

	doc, err := htmlquery.Parse(strings.NewReader(htmlResp.String()))
	if err != nil {
		return fmt.Errorf("HTML 解析错误: %w", err)
	}

	// 构造提交参数
	vals := url.Values{}
	vals.Set("uId", c.uid)
	vals.Set("fid", c.fid)
	vals.Set("questionnaireId", qID)
	vals.Set("alreadyId", alreadyID)
	vals.Set("grantId", grantID)
	vals.Set("saveType", "2") // 2 = 提交, 1 = 保存

	// 查找所有的题目块 "subjectBox"
	// 必须按块处理，才能通过标题区分哪些题该打分，哪些题该留空
	subjectBoxes := htmlquery.Find(doc, "//div[@class='subjectBox']")

	foundAny := false

	for _, box := range subjectBoxes {
		// 获取当前板块的标题 ("指标一：学生评价" 或 "指标二：主观内容")
		h2 := htmlquery.FindOne(box, "./h2")
		if h2 == nil {
			continue
		}
		title := strings.TrimSpace(htmlquery.InnerText(h2))

		// 判断是否为主观题：如果标题包含 "主观"，则说明这是主观题板块
		isSubjective := strings.Contains(title, "主观")

		// 在当前板块内查找题目 ID (hidden input)
		inputs := htmlquery.Find(box, ".//div[contains(@class, 'groupTarget')]/input[@type='hidden']")

		for _, input := range inputs {
			id := htmlquery.SelectAttr(input, "value")

			// 过滤掉长度小于6的无效 ID (这是为了解决之前出现的短 ID 干扰问题)
			if len(id) < 6 {
				continue
			}

			foundAny = true
			vals.Add("groupTargetIds", id)
			vals.Set(id+"_chooseSetUp", "1")

			if isSubjective {
				// 主观题逻辑：类型4，内容留空
				vals.Set(id+"_type", "4")
				vals.Set(id, "")
			} else {
				// 客观题逻辑：类型5，打20分
				vals.Set(id+"_type", "5")
				vals.Set(id, "20")
			}
		}
	}

	if !foundAny {
		return fmt.Errorf("未在 HTML 中找到有效的题目 ID")
	}

	// 提交表单

	res, err := c.http.R().
		SetQueryParamsFromValues(vals).
		Post("https://newes.chaoxing.com/pj/newesReception/saveQuestionnaire")

	if err != nil {
		return err
	}

	if !strings.Contains(res.String(), "1") && !strings.Contains(res.String(), "true") {
		return fmt.Errorf("服务器返回异常: %s", res.String())
	}

	return nil
}

func main() {
	var phone, password string

	fmt.Println("========================================")
	fmt.Println("         超星(学习通) 自动评教          ")
	fmt.Println("========================================")

	// 获取用户输入
	fmt.Print("请输入手机号: ")
	fmt.Scanln(&phone)

	fmt.Print("请输入密码: ")
	fmt.Scanln(&password)

	if phone == "" || password == "" {
		log.Fatal("账号或密码不能为空！")
	}

	// 初始化客户端
	client := NewClient()

	log.Println("[主程序] 正在登录...")
	if err := client.Login(phone, password); err != nil {
		log.Fatalf("[致命错误] 登录失败: %v", err)
	}
	log.Println("[主程序] 登录成功！")

	log.Println("[主程序] 正在获取当前学期...")
	semesterID, err := client.GetSemesterID()
	if err != nil {
		log.Fatalf("[致命错误] 获取学期失败: %v", err)
	}

	log.Printf("[主程序] 当前学期 ID: %s", semesterID)

	// 开始执行评教任务
	client.ProcessQuestionnaires(semesterID)

	log.Println("[主程序] 所有任务已完成。")

	// 防止程序跑完直接关闭窗口（Windows下常见需求）
	fmt.Println("\n按回车键退出...")
	fmt.Scanln()
}
