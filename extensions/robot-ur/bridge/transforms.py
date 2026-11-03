"""Small SO(3) helpers: UR poses use rotation vectors, not Euler angles."""
import math


def quaternion(v):
    angle = math.hypot(*v)
    scale = math.sin(angle / 2) / angle if angle > 1e-12 else .5
    return [math.cos(angle / 2)] + [x * scale for x in v]


def multiply(a, b):
    w, x, y, z = a
    v, i, j, k = b
    return [w*v-x*i-y*j-z*k, w*i+x*v+y*k-z*j,
            w*j-x*k+y*v+z*i, w*k+x*j-y*i+z*v]


def rotation_vector(q):
    if q[0] < 0:
        q = [-x for x in q]
    length = math.hypot(*q[1:])
    scale = 2 * math.atan2(length, q[0]) / length if length > 1e-12 else 2
    return [v * scale for v in q[1:]]


def relative(pose, delta, frame):
    q, d = quaternion(pose[3:]), quaternion(delta[3:])
    xyz = delta[:3]
    if frame == "tool":
        xyz = multiply(multiply(q, [0] + xyz), [q[0]] + [-v for v in q[1:]])[1:]
        orientation = multiply(q, d)
    else:
        orientation = multiply(d, q)
    return [a + b for a, b in zip(pose[:3], xyz)] + rotation_vector(orientation)
