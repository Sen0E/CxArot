import sys
from collections import defaultdict

import requests
from lxml import etree
from loguru import logger

logger.add('AROT.log',
           format="{time:YYYY-MM-DD HH:mm:ss} | {level} | {function}:{line} | {message}",
           rotation='5 MB',
           encoding='utf-8')


class AcademicReviewOfTeaching:
    def __init__(self, phone, password):
        self.phone = phone
        self.password = password
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 ("
                          "KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36 "
                          "Edg/137.0.0.0"})
    def login(self):
        login_url = "https://passport2.chaoxing.com/fanyalogin"
        login_params = {'fid': -1,
                        'referer': 'http://i.mooc.chaoxing.com/space/index',
                        'uname': self.phone,
                        'password': self.password}
        logger.debug(self.phone+" - "+str(login_params))
        login_response = self.session.post(url=login_url, params=login_params)
        logger.debug(self.phone+" - "+login_response.text)
        if login_response.status_code == 200:
            login_response_json = login_response.json()
            if login_response_json.get('status'):
                logger.debug(self.phone+" - "+str(self.session.cookies))
                return self.session.cookies
            else:
                raise Exception("login failed" + " - " + login_response_json.get('msg2'))
        else:
            raise Exception("login failed")

    def get_all_semesters(self):
        semesters_url = "https://newes.chaoxing.com/pj/semesterV2/findSemesterList"
        all_semesters_response = self.session.get(url=semesters_url)
        logger.debug(self.phone+" - "+all_semesters_response.text)
        if all_semesters_response.status_code == 200:
            all_semesters_response_json = all_semesters_response.json()
            if all_semesters_response_json.get('status'):
                return all_semesters_response_json.get('data')
            else:
                raise Exception("get all semesters failed")

    def get_questionnaire(self, semester_id):
        questionnaire_url = "https://newes.chaoxing.com/pj/newesReceptionV2/GetMyEvaluationList"
        questionnaire_params = {
            'evaluateObjType': '',
            'semesterId': semester_id,
            'title': '',
            'sort': 2,
            'pageIndex': 1,
            'pageSize': 10
        }
        logger.debug(self.phone+" - "+str(questionnaire_params))
        questionnaire_response = self.session.get(url=questionnaire_url, params=questionnaire_params)
        logger.debug(self.phone+" - "+questionnaire_response.text)
        if questionnaire_response.status_code == 200:
            questionnaire_response_json = questionnaire_response.json()
            if questionnaire_response_json.get('status'):
                return questionnaire_response_json.get('data').get('list')
            else:
                raise Exception("get questionnaire failed")

    def get_evaluation(self, questionnaire_id):
        evaluation_url = "https://newes.chaoxing.com/pj/newesReceptionV2/GetMyEvaluationQuestionnaireById"
        evaluation_url_params = {
            'questionnaireId': questionnaire_id,
            'pageIndex': 1,
            'pageSize': 10000,
            'kw': ''
        }
        logger.debug(self.phone+" - "+str(evaluation_url_params))
        evaluation_response = self.session.get(url=evaluation_url, params=evaluation_url_params)
        if evaluation_response.status_code == 200:
            evaluation_response_json = evaluation_response.json()
            if evaluation_response_json.get('status'):
                return evaluation_response_json.get('data').get('list')
            else:
                raise Exception("get evaluation failed")

    def get_evaluation_id(self, f_id, u_id, already_id, grant_id, questionnaire_id):
        evaluation_html_url = "https://newes.chaoxing.com/pj/newesReception/questionnaireInfo"
        evaluation_html_params = {
            "fid": f_id,
            "uId": u_id,
            "alreadyId": already_id,
            "grantId": grant_id,
            "questionnaireId": questionnaire_id,
            "type": "1",
            "classificationId": "",
            "isNewPage": "true",
            "source": "14"
        }
        logger.debug(self.phone+" - "+str(evaluation_html_params))
        html_response = self.session.get(url=evaluation_html_url, params=evaluation_html_params)
        if html_response.status_code == 200:
            evaluation_html = etree.HTML(html_response.text)
            evaluation_data = {}
            evaluation_see = {}
            for i, v in enumerate(evaluation_html.xpath('//*[@class="subjectBox"]/h2')):
                evaluation_data[v.text] = [i.get('value') for i in evaluation_html.xpath(
                    f'//div[@class="subjectBox"][{i + 1}]/div[@class="testBox groupTarget"]/input[1]')]
                evaluation_see[v.text] = [i.get('value') for i in evaluation_html.xpath(
                    f'//div[@class="subjectBox"][{i + 1}]/div[@class="testBox groupTarget"]//span[@class="target-title"]')]
            return evaluation_data, evaluation_see

    def save_question(self, u_id, f_id, questionnaire_id, already_id, grant_id, evaluation_data):
        save_question_url = 'https://newes.chaoxing.com/pj/newesReception/saveQuestionnaire'

        # 检查 evaluation_data 中是否包含所需的键
        student_evaluation = evaluation_data.get('指标一：学生评价', [])
        subjective_content = evaluation_data.get('指标二：主观内容', [])

        # 分步骤生成 raw_params
        group_targets = [{'groupTargetIds': i} for i in student_evaluation + subjective_content]
        student_types = [{f"{i}_type": 5} for i in student_evaluation]
        subjective_type = [{f"{subjective_content}_type": 4}] if subjective_content else []
        choose_setups = [{f"{i}_chooseSetUp": 1} for i in student_evaluation + subjective_content]
        student_scores = [{i: 20} for i in student_evaluation]
        subjective_empty = [{subjective_content[0]: ''}] if subjective_content else []

        raw_params = group_targets + student_types + subjective_type + choose_setups + student_scores + subjective_empty

        group_data = defaultdict(list)
        group_target_ids = []

        for item in raw_params:
            for k, v in item.items():
                k_str = str(k)
                if k_str == 'groupTargetIds':
                    group_target_ids.append(str(v))
                else:
                    group_id = k_str.split('_')[0] if '_' in k_str else k_str
                    group_data[group_id].append((k_str, v))

        # 初始化基本参数
        params = [
            ('uId', u_id), ('fid', f_id), ('questionnaireId', questionnaire_id),
            ('alreadyId', already_id), ('grantId', grant_id), ('jumpInfo', ''),
            ('saveType', 2), ('submitreasons', ''),
            ('submit_highscore_reasons', ''), ('submit_lowscore_reasons', '')
        ]

        # 添加 groupTargetIds 参数
        params.extend(('groupTargetIds', gid) for gid in sorted(group_target_ids, key=str))

        # 添加 group_data 中的参数
        for gid in sorted(group_data.keys(), key=str):
            params.extend(group_data[gid])

        logger.debug(self.phone+" - "+str(params))
        response = self.session.post(url=save_question_url, params=params)
        if response.status_code != 200:
            raise Exception(f"Failed to save question. Status code: {response.status_code}")
        return response.json()

    def task(self):
        self.login()

        semesters = self.get_all_semesters()
        for semester in semesters:
            if semester.get('iscurrent') == 1:
                logger.info(self.phone+" - "+"当前学期为:"+semester['name'])
                semester_id = semester.get('id')
                questionnaires = self.get_questionnaire(semester_id)
                for questionnaire in questionnaires:
                    logger.info(self.phone+" - "+"问卷名称:"+questionnaire.get('name')+
                                " 开始时间:"+questionnaire.get('startTime')+
                                " 结束时间:"+questionnaire.get('endTime'))
                    questionnaire_id = questionnaire.get('questionnaireId')
                    evaluations = self.get_evaluation(questionnaire_id)
                    u_id = self.session.cookies.get_dict().get('UID')
                    f_id = self.session.cookies.get_dict().get('fid')
                    logger.info(self.phone+" - "+"开始评教")
                    for evaluation in evaluations:
                        if evaluation.get('submitStatus') != 2:
                            logger.info(self.phone+" - "+"教师名称:"+evaluation.get('alreadyObject').get('teacherName')+
                                        " 课程名称:"+evaluation.get('alreadyObject').get('courseName')+
                                        " 学院:"+evaluation.get('alreadyObject').get('teacherDepartmentName'))
                            already_id = evaluation.get('alreadyObjectId')
                            grant_id = evaluation.get('grantId')
                            evaluation_data, see = self.get_evaluation_id(f_id, u_id, already_id, grant_id,
                                                                          questionnaire_id)
                            print(see)
                            self.save_question(u_id, f_id, questionnaire_id, already_id, grant_id, evaluation_data)
        logger.info(self.phone+" - "+"评教完成")
