def run(c):
    teacher = c.values["teacher"]["access_token"]
    student = c.values["student_a"]["access_token"]
    class_a = c.values["teacher"]["user"]["classId"]
    class_b = c.values["student_b"]["user"]["classId"]
    name = c.unique("提示词模板")
    body = {"name":name,"content":"请引导 {{student_name}} 思考 {{class_name}} 的问题。","enabled":True}
    status, _, denied = c.request("POST", "/api/prompt-templates", body, student)
    assert status == 403 and "error" in denied
    status, _, created = c.request("POST", "/api/prompt-templates", body, teacher)
    assert status == 201, (status, created)
    template = created["template"]
    assert template["class_id"] == class_a and template["name"] == name
    assert c.db_scalar(f"SELECT content FROM prompt_templates WHERE id={template['id']} AND class_id={class_a};") == body["content"]
    status, _, duplicate = c.request("POST", "/api/prompt-templates", body, teacher)
    assert status == 400 and "同名" in duplicate["error"]
    status, _, invalid = c.request("POST", "/api/prompt-templates", {**body,"name":c.unique("非法"),"content":"{{secret}}"}, teacher)
    assert status == 400 and "error" in invalid
    other_id = int(c.db_scalar(f"SELECT id FROM prompt_templates WHERE class_id={class_b} ORDER BY id LIMIT 1;"))
    status, _, cross = c.request("GET", f"/api/prompt-templates/{other_id}", token=teacher)
    status2, _, missing = c.request("GET", "/api/prompt-templates/9223372036854770000", token=teacher)
    assert status == status2 == 404 and cross == missing
    agent_body = {"name":c.unique("模板绑定智能体"),"prompt":"","enabled":True,"skill_ids":[],"material_ids":[],"prompt_template_id":template["id"]}
    status, _, agent_result = c.request("POST", "/api/agents", agent_body, teacher)
    assert status == 201 and agent_result["agent"]["prompt_template_id"] == template["id"], (status,agent_result)
    assert c.db_scalar(f"SELECT prompt_template_id FROM tutor_agents WHERE id={agent_result['agent']['id']};") == str(template["id"])
    status, _, cross_bind = c.request("PUT", f"/api/agents/{agent_result['agent']['id']}", {**agent_body,"prompt_template_id":other_id}, teacher)
    assert status == 404 and "error" in cross_bind
    status, _, unchanged = c.request("GET", f"/api/agents/{agent_result['agent']['id']}", token=teacher)
    assert status == 200 and unchanged["agent"]["prompt_template_id"] == template["id"]
    return "模板 CRUD、变量白名单、学生 403、跨班 404、智能体绑定原子性和持久化通过"
