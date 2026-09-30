import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.nio.charset.StandardCharsets;
import org.apache.lucene.analysis.LowerCaseFilter;
import org.apache.lucene.analysis.StopFilter;
import org.apache.lucene.analysis.TokenStream;
import org.apache.lucene.analysis.en.EnglishAnalyzer;
import org.apache.lucene.analysis.en.EnglishPossessiveFilter;
import org.apache.lucene.analysis.en.PorterStemFilter;
import org.apache.lucene.analysis.standard.StandardTokenizer;
import org.apache.lucene.analysis.tokenattributes.CharTermAttribute;

// Reads one text per line from stdin and prints its analyzed tokens, space-separated,
// using the same filter chain as Anserini's DefaultEnglishAnalyzer.
public class LuceneAnalyze {
  public static void main(String[] args) throws IOException {
    BufferedReader in = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
    PrintWriter out = new PrintWriter(System.out, false, StandardCharsets.UTF_8);
    String line;
    while ((line = in.readLine()) != null) {
      out.println(String.join(" ", analyze(line)));
    }
    out.flush();
  }

  static java.util.List<String> analyze(String text) throws IOException {
    StandardTokenizer tokenizer = new StandardTokenizer();
    tokenizer.setReader(new StringReader(text));
    TokenStream stream = new EnglishPossessiveFilter(tokenizer);
    stream = new LowerCaseFilter(stream);
    stream = new StopFilter(stream, EnglishAnalyzer.ENGLISH_STOP_WORDS_SET);
    stream = new PorterStemFilter(stream);

    java.util.List<String> tokens = new java.util.ArrayList<>();
    CharTermAttribute term = stream.addAttribute(CharTermAttribute.class);
    stream.reset();
    while (stream.incrementToken()) {
      tokens.add(term.toString());
    }
    stream.end();
    stream.close();
    return tokens;
  }
}
