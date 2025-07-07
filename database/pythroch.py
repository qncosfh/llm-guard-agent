import torch
from torch.utils.data import Dataset, DataLoader
from transformers import BertTokenizer, BertModel
import torch.nn as nn
import pandas as pd
from sklearn.preprocessing import MultiLabelBinarizer
from sklearn.model_selection import train_test_split

# 1. 读取标签顺序
with open("labels.txt", "r", encoding="utf-8") as f:
    label_names = [line.strip() for line in f if line.strip()]

# 2. 数据加载
df = pd.read_csv("train_data.csv")
df["labels"] = df["labels"].apply(lambda x: x.split(","))
mlb = MultiLabelBinarizer(classes=label_names)
y = mlb.fit_transform(df["labels"])

# 2. 数据集
class TextDataset(Dataset):
    def __init__(self, texts, labels, tokenizer, max_len=32):
        self.texts = texts
        self.labels = labels
        self.tokenizer = tokenizer
        self.max_len = max_len
    def __len__(self):
        return len(self.texts)
    def __getitem__(self, idx):
        enc = self.tokenizer(self.texts[idx], truncation=True, padding='max_length', max_length=self.max_len, return_tensors='pt')
        return {k: v.squeeze(0) for k, v in enc.items()}, torch.tensor(self.labels[idx], dtype=torch.float32)

tokenizer = BertTokenizer.from_pretrained("bert-base-chinese")
X_train, X_test, y_train, y_test = train_test_split(df["text"], y, test_size=0.1, random_state=42)
train_ds = TextDataset(list(X_train), y_train, tokenizer)
test_ds = TextDataset(list(X_test), y_test, tokenizer)
train_loader = DataLoader(train_ds, batch_size=16, shuffle=True)
test_loader = DataLoader(test_ds, batch_size=16)

# 3. 模型
class BertForMultiLabel(nn.Module):
    def __init__(self, num_labels):
        super().__init__()
        self.bert = BertModel.from_pretrained("bert-base-chinese")
        self.classifier = nn.Linear(self.bert.config.hidden_size, num_labels)
    def forward(self, input_ids, attention_mask, token_type_ids):
        outputs = self.bert(input_ids=input_ids, attention_mask=attention_mask, token_type_ids=token_type_ids)
        pooled = outputs.pooler_output
        logits = self.classifier(pooled)
        return torch.sigmoid(logits)

device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
model = BertForMultiLabel(len(label_names)).to(device)
optimizer = torch.optim.AdamW(model.parameters(), lr=2e-5)
loss_fn = nn.BCELoss()

# 4. 训练
for epoch in range(2):  # 小样本2轮即可
    model.train()
    for batch in train_loader:
        inputs, labels = batch
        for k in inputs:
            inputs[k] = inputs[k].to(device)
        labels = labels.to(device)
        optimizer.zero_grad()
        outputs = model(**inputs)
        loss = loss_fn(outputs, labels)
        loss.backward()
        optimizer.step()
    print(f"Epoch {epoch+1} done.")

# 5. 导出ONNX
model.eval()
dummy_inputs = tokenizer("你好，法轮功是什么", return_tensors="pt", max_length=32, padding="max_length", truncation=True)
input_names = ["input_ids", "attention_mask", "token_type_ids"]
torch.onnx.export(
    model,
    (dummy_inputs["input_ids"], dummy_inputs["attention_mask"], dummy_inputs["token_type_ids"]),
    "content_guard.onnx",
    input_names=input_names,
    output_names=["output"],
    dynamic_axes={k: {0: "batch"} for k in input_names},
    opset_version=17   # 推荐用17，14及以上都可以
)
print("ONNX模型已导出为 content_guard.onnx")
print("标签顺序：", label_names)