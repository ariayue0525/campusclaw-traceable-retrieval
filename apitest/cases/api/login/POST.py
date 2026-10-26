import os
from common import require_fields


def run(c):
    teacher = c.login("teacher-a", os.environ["SEED_TEACHER_PASSWORD"])
    student_a = c.login("student-a1", os.environ["SEED_STUDENT_A_PASSWORD"])
    student_b = c.login("student-b1", os.environ["SEED_STUDENT_B_PASSWORD"])
    require_fields(teacher, "access_token", "refresh_token", "user")
    assert teacher["user"]["role"] == "teacher"
    assert student_a["user"]["classId"] != student_b["user"]["classId"]
    c.values.update(teacher=teacher, student_a=student_a, student_b=student_b)
    return "三类账号登录成功，Bearer/refresh/user 字段与班级断言通过"
