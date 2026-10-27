import json
import urllib.request


def source(name, enabled="是"):
    return f"名称：{name}\n适用条件：方程,应用题\n资料：数学\n工具：knowledge_search\n启用：{enabled}\n\n先询问学生卡在何处，不直接给答案。"


def run(c):
    teacher = c.values["teacher"]["access_token"]
    student = c.values["student_a"]["access_token"]
    class_a = c.values["teacher"]["user"]["classId"]
    class_b = c.values["student_b"]["user"]["classId"]
    name = c.unique("技能接口测试")
    payload = {"source_text": source(name)}
    status, _, denied = c.request("POST", "/api/skills", payload, student)
    assert status == 403 and "error" in denied
    status, _, body = c.request("POST", "/api/skills", payload, teacher)
    assert status == 201, (status, body)
    skill = body["skill"]
    assert skill["name"] == name and skill["class_id"] == class_a
    assert skill["tools"] == ["knowledge_search"] and skill["enabled"] is True
    assert c.db_scalar(f"SELECT description FROM skills WHERE id={skill['id']} AND class_id={class_a};") == skill["description"]
    status, _, duplicate = c.request("POST", "/api/skills", payload, teacher)
    assert status == 400 and "同名" in duplicate["error"]
    invalid = {"source_text": source(c.unique("非法技能")).replace("启用：是", "名称：重复\n启用：是")}
    status, _, rejected = c.request("POST", "/api/skills", invalid, teacher)
    assert status == 400 and "error" in rejected
    other_id = int(c.db_scalar(f"SELECT id FROM skills WHERE class_id={class_b} ORDER BY id LIMIT 1;"))
    status, _, cross = c.request("GET", f"/api/skills/{other_id}", token=teacher)
    status2, _, missing = c.request("GET", "/api/skills/9223372036854770000", token=teacher)
    assert status == status2 == 404 and cross == missing
    status, _, cross_update = c.request("PUT", f"/api/skills/{other_id}", payload, teacher)
    assert status == 404 and cross_update == missing
    status, _, updated = c.request("PUT", f"/api/skills/{skill['id']}", {"source_text": source(name, "否")}, teacher)
    assert status == 200 and updated["skill"]["enabled"] is False
    assert c.db_scalar(f"SELECT enabled FROM skills WHERE id={skill['id']};") == "0"
    status, _, listing = c.request("GET", "/api/skills", token=teacher)
    assert status == 200 and any(s["id"] == skill["id"] for s in listing["skills"])
    upload_name = c.unique("上传技能测试")
    boundary = "campusclaw-apitest-boundary"
    raw = source(upload_name).encode("utf-8")
    multipart = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"skill.txt\"\r\nContent-Type: text/plain\r\n\r\n".encode() + raw + f"\r\n--{boundary}--\r\n".encode())
    request = urllib.request.Request(c.base + "/api/skills", data=multipart, method="POST", headers={"Authorization":"Bearer " + teacher,"Content-Type":"multipart/form-data; boundary=" + boundary})
    with urllib.request.urlopen(request, timeout=40) as response:
        uploaded = json.load(response)
        assert response.status == 201
    assert uploaded["skill"]["name"] == upload_name
    assert c.db_scalar(f"SELECT COUNT(*) FROM skills WHERE id={uploaded['skill']['id']} AND class_id={class_a};") == "1"
    return "教师技能 CRUD、multipart 文件上传、严格解析、学生 403、跨班 404、数据库持久化均通过"
