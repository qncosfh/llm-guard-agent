from fastapi import FastAPI
from pydantic import BaseModel
import onnxruntime as ort
from transformers import BertTokenizer
import numpy as np
import os

app = FastAPI()

# 加载分词器和ONNX模型
# tokenizer = BertTokenizer.from_pretrained("bert-base-chinese")  # 默认huggingFace分词器
tokenizer = BertTokenizer.from_pretrained("./hf_cache/bert-base-chinese-offline")
session = ort.InferenceSession("content_guard.onnx")            # 你的ONNX模型路径
# 自动读取labels.txt
with open(os.path.join(os.path.dirname(__file__), "labels.txt"), "r", encoding="utf-8") as f:
    label_names = [line.strip() for line in f if line.strip()]

class Query(BaseModel):
    text: str

@app.post("/predict")
def predict(query: Query):
    # 分词
    inputs = tokenizer(
        query.text,
        return_tensors="np",
        max_length=32,           # 和你模型输入一致
        padding="max_length",
        truncation=True
    )
    # ONNX推理
    ort_inputs = {k: v for k, v in inputs.items()}
    ort_outs = session.run(None, ort_inputs)
    probs = ort_outs[0][0]      # [num_labels]
    result = []
    for i, p in enumerate(probs):
        if p > 0.5:
            result.append(label_names[i])
    return {
        "blocked": bool(result),
        "labels": result,
        "probs": probs.tolist()
    }